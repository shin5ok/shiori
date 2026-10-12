package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"my-bookmark/internal/cost"
	"my-bookmark/internal/store"
)

func TestGenerationCostsRenderedAndPolled(t *testing.T) {
	a := testApp(t)
	id := strings.Repeat("c", 64)
	entry := store.Article{ID: id, URL: "https://example.com", Title: "記事", Status: "ready", Points: []string{"要点"}, SummaryCost: cost.Estimate{USD: .001, Requests: 1}, InfographicCost: cost.Estimate{USD: .1364, Requests: 3}}
	a.db = &infographicTestDB{article: entry}
	r := httptest.NewRequest("GET", "/articles/"+id, nil)
	r.AddCookie(&http.Cookie{Name: "shiori_session", Value: "token"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("page status: %d", w.Code)
	}
	for _, text := range []string{"generation-cost summary-cost", "generation-cost infographic-cost", "推定コスト：約¥0.15", "推定コスト：約¥20.46", "1ドル150円"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("missing %q", text)
		}
	}
	data := summaryData(entry)
	if data["summary_cost"] != entry.SummaryCost.Label() || data["infographic_cost"] != entry.InfographicCost.Label() {
		t.Fatalf("polling costs: %+v", data)
	}
}
