package content

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"my-bookmark/internal/cost"
)

func TestGeminiRecordsChargedIncompleteResponse(t *testing.T) {
	for _, status := range []int{200, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":100},"candidates":[{"finishReason":"MAX_TOKENS"}]}`))
			}))
			defer server.Close()
			g := &Gemini{Project: "test", Location: "global", Model: "gemini-3.8-flash", Endpoint: server.URL, TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test"})}
			var recorded []cost.Estimate
			ctx := cost.WithRecorder(context.Background(), func(e cost.Estimate) { recorded = append(recorded, e) })
			_, err := g.generateJSON(ctx, nil, "", nil, time.Second)
			if err == nil {
				t.Fatal("expected incomplete/API error")
			}
			if status == 200 && (len(recorded) != 1 || recorded[0].USD <= 0 || recorded[0].Unknown != 0) {
				t.Fatalf("charged usage lost: %+v", recorded)
			}
			if status != 200 && len(recorded) != 0 {
				t.Fatal("HTTP errors must not be charged")
			}
		})
	}
}
