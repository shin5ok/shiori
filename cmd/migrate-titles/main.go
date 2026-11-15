package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"my-bookmark/internal/content"
)

func main() {
	project := flag.String("project", "", "Google Cloud project to migrate")
	location := flag.String("gemini-location", env("GEMINI_LOCATION", "global"), "Vertex AI location for titles of meaningless pages")
	model := flag.String("gemini-model", env("GEMINI_MODEL", "gemini-3.8-flash"), "Gemini model for titles of meaningless pages")
	dryRun := flag.Bool("dry-run", false, "print the changes without writing them (no Gemini calls; AI titles show as placeholders)")
	flag.Parse()
	if *project == "" {
		log.Fatal("--project is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	c, err := firestore.NewClient(ctx, *project)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	var gemini *content.Gemini
	if !*dryRun {
		tokens, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			log.Fatal(err)
		}
		gemini = &content.Gemini{Project: *project, Location: *location, Model: *model, TokenSource: tokens}
	}
	fetcher := content.NewFetcher()
	var changed, same, skipped int
	docs := c.Collection("articles").Documents(ctx)
	defer docs.Stop()
	for {
		d, err := docs.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		data := d.Data()
		url, _ := data["url"].(string)
		current, _ := data["title"].(string)
		// Videos are summarized without fetching the page, so their generated title stays.
		if _, isVideo, _ := content.YouTubeURL(url); isVideo || url == "" {
			skipped++
			continue
		}
		title, ok := newTitle(ctx, fetcher, gemini, url, current, stringList(data["tldr"]), stringList(data["points"]))
		if !ok {
			skipped++
			continue
		}
		if title == current {
			same++
			continue
		}
		log.Printf("%s: %q -> %q", d.Ref.ID, current, title)
		if !*dryRun {
			if _, err := d.Ref.Update(ctx, []firestore.Update{{Path: "title", Value: title}}); err != nil {
				log.Fatal(err)
			}
		}
		changed++
		time.Sleep(200 * time.Millisecond)
	}
	log.Printf("titles changed=%d unchanged=%d skipped=%d dry-run=%t", changed, same, skipped, *dryRun)
}

// newTitle decides the stored title. A meaningless page title (a bare URL, the
// host name) or a non-Japanese one is replaced by a Gemini title written from
// the stored summary and marked as generated. In a dry run (no Gemini client)
// such titles are only reported.
func newTitle(ctx context.Context, fetcher *http.Client, gemini *content.Gemini, url, current string, tldr, points []string) (string, bool) {
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	doc, err := content.FetchDocument(fetchCtx, fetcher, url)
	title := content.ShortenTitle(doc.Title)
	if err != nil {
		log.Printf("skip %s: %v", url, err)
		title = current
	}
	meaningless := content.IsMeaninglessTitle(title, url)
	if meaningless && content.IsMarkedGenerated(current) {
		return current, true
	}
	if err != nil && !meaningless {
		return "", false
	}
	if meaningless || !content.IsJapanese(title) {
		if len(tldr)+len(points) > 0 {
			if gemini == nil {
				return `"(AIで生成)"`, true
			}
			generated, gerr := gemini.TitleFromSummary(ctx, tldr, points)
			if gerr != nil {
				log.Printf("title for %s not generated: %v", url, gerr)
			} else if generated != "" {
				return content.MarkGenerated(generated), true
			}
		}
		// Without a generated title, a Japanese stored title is at least marked as AI-written.
		if !meaningless && content.IsJapanese(current) && !content.IsMeaninglessTitle(current, url) && !content.IsMarkedGenerated(current) {
			return content.MarkGenerated(current), true
		}
		return current, true
	}
	return title, true
}

func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
