package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"my-bookmark/internal/content"
)

func main() {
	project := flag.String("project", "", "Google Cloud project to migrate")
	dryRun := flag.Bool("dry-run", false, "print the changes without writing them")
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
		url, _ := d.Data()["url"].(string)
		current, _ := d.Data()["title"].(string)
		// Videos are summarized without fetching the page, so their generated title stays.
		if _, isVideo, _ := content.YouTubeURL(url); isVideo || url == "" {
			skipped++
			continue
		}
		title, ok := pageTitle(ctx, fetcher, url)
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

func pageTitle(ctx context.Context, fetcher *http.Client, url string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	doc, err := content.FetchDocument(ctx, fetcher, url)
	if err != nil {
		log.Printf("skip %s: %v", url, err)
		return "", false
	}
	title := content.ShortenTitle(doc.Title)
	return title, title != ""
}
