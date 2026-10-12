package content

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"golang.org/x/oauth2"
	"my-bookmark/internal/summary"
)

func TestGeminiGeneratesTagsFromFixedVocabulary(t *testing.T) {
	candidates := []string{"AI", "技術", "クラウド", "SRE", "OSS"}
	for _, tags := range [][]string{{"AI", "技術"}, {"クラウド", "SRE", "OSS"}, {"AI", "勝手なタグ"}, {"AI", "AI"}} {
		t.Run(tags[0]+tags[1], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					GenerationConfig struct {
						Schema struct {
							Properties map[string]struct{ Items struct{ Enum []string } }
						} `json:"responseJsonSchema"`
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if got := request.GenerationConfig.Schema.Properties["tags"].Items.Enum; !reflect.DeepEqual(got, candidates) {
					t.Errorf("tag vocabulary: %v", got)
				}
				output, _ := json.Marshal(map[string]any{"sufficient": true, "title": "日本語の記事タイトル", "points": []string{"要点一", "要点二"}, "tldr": []string{"結論です", "理由です"}, "tags": tags})
				_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]string{"text": string(output)}}}}}})
			}))
			defer server.Close()
			g := &Gemini{TagCandidates: candidates, Project: "test", Location: "global", Model: "gemini-3.8-flash", Endpoint: server.URL, TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test"})}
			result, err := g.Summarize(context.Background(), "記事", "記事本文", summary.Standard, "")
			if err != nil {
				t.Fatal(err)
			}
			if summary.ValidAutoTags(tags, candidates) {
				if !reflect.DeepEqual(result.Tags, tags) {
					t.Fatalf("tags: %v", result.Tags)
				}
			} else if len(result.Tags) != 0 {
				t.Fatalf("invalid tags escaped validation: %v", result.Tags)
			}
		})
	}
}
