package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"my-bookmark/internal/store"
)

func TestAPIResponseNegotiation(t *testing.T) {
	for _, tc := range []struct {
		accept string
		html   bool
	}{
		{"", false}, {"*/*", false}, {"application/json", false},
		{"text/html", true}, {"text/html,application/xhtml+xml,*/*;q=0.8", true},
		{"text/html;q=0", false}, {"text/html;q=invalid", false},
		{"text/html;q=0.5,application/json;q=1", false},
		{"application/json;q=0.5,text/html;q=1", true},
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			t.Run(method+tc.accept, func(t *testing.T) {
				a, _, raw := apiFixture(t)
				r := httptest.NewRequest(method, "/api/bookmarks?token="+raw, strings.NewReader(`{"url":"https://example.com/article"}`))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Accept", tc.accept)
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, r)
				if w.Code != 200 || w.Header().Get("Vary") != "Accept" {
					t.Fatalf("status=%d headers=%v", w.Code, w.Header())
				}
				if tc.html {
					if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "ブックマークを登録しました") || !strings.Contains(w.Body.String(), `href="https://shiori.example/"`) {
						t.Fatalf("HTML result: %s", w.Body.String())
					}
					if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-") {
						t.Fatal("missing style nonce")
					}
				} else {
					var result map[string]string
					if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result["redirect_url"] != "https://shiori.example/" {
						t.Fatalf("result=%v", result)
					}
				}
				if strings.Contains(w.Body.String(), raw) || w.Header().Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("token/referrer leak")
				}
			})
		}
	}
}

func TestAPIHTMLErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		status       int
		missingToken bool
		saveErr      error
	}{
		{name: "missing token", status: 401, missingToken: true},
		{name: "invalid JSON", body: "invalid", status: 400},
		{name: "quota", body: `{"url":"https://example.com"}`, status: 429, saveErr: store.ErrRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, db, raw := apiFixture(t)
			db.saveErr = tc.saveErr
			target := "/api/bookmarks"
			if !tc.missingToken {
				target += "?token=" + raw
			}
			r := httptest.NewRequest("POST", target, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "text/html")
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), "登録できませんでした") || !strings.Contains(w.Body.String(), `href="https://shiori.example/"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), raw) {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestAPIHTMLEscapesArticleContent(t *testing.T) {
	a, _, _ := apiFixture(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/bookmarks", nil)
	r.Header.Set("Accept", "text/html")
	a.apiResponse(w, r, 200, map[string]string{"title": `<script>alert(1)</script>`, "url": "javascript:alert(1)", "article_url": "https://shiori.example/articles/example"})
	if strings.Contains(w.Body.String(), "<script>") || strings.Contains(w.Body.String(), `href="javascript:`) || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("unsafe HTML: %s", w.Body.String())
	}
}
