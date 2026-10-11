package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"my-bookmark/internal/infographic"
	"my-bookmark/internal/store"
)

type infographicTestDB struct {
	testDB
	article  store.Article
	enqueues int
	picture  infographic.Image
}

func (d *infographicTestDB) GetInfographicImage(context.Context, string, string) (infographic.Image, error) {
	return d.picture, nil
}

func (d *infographicTestDB) Get(context.Context, string) (store.Article, error) {
	return d.article, nil
}
func (d *infographicTestDB) EnqueueInfographic(context.Context, string, string) error {
	d.enqueues++
	d.article.InfographicStatus = "queued"
	return nil
}
func (d *infographicTestDB) Own(context.Context, string, string) (*store.Bookmark, error) {
	return &store.Bookmark{}, nil
}
func (d *infographicTestDB) Comments(context.Context, string) ([]store.Bookmark, error) {
	return nil, nil
}

func TestInfographicRequiresSessionAndCSRF(t *testing.T) {
	a := testApp(t)
	id := strings.Repeat("a", 64)
	db := &infographicTestDB{article: store.Article{ID: id, Status: "ready", Points: []string{"要点"}}}
	a.db = db
	for _, tc := range []struct {
		cookie, csrf string
		want         int
	}{{"", "correct", 401}, {"token", "wrong", 403}, {"token", "correct", 202}} {
		r := httptest.NewRequest("POST", "/articles/"+id+"/infographic", strings.NewReader(url.Values{"csrf": {tc.csrf}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if tc.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "shiori_session", Value: tc.cookie})
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("status=%d want=%d", w.Code, tc.want)
		}
	}
	if db.enqueues != 1 {
		t.Fatalf("enqueues=%d", db.enqueues)
	}
}

func TestInfographicIsRenderedAsEscapedSVG(t *testing.T) {
	a := testApp(t)
	id := strings.Repeat("b", 64)
	plan := &infographic.Plan{Title: "図解", Takeaway: "結論", Kind: "cards", Items: []infographic.Item{{Heading: "一", Detail: "<script>"}, {Heading: "二", Detail: "説明"}, {Heading: "三", Detail: "説明"}}}
	a.db = &infographicTestDB{article: store.Article{ID: id, Status: "ready", InfographicStatus: "ready", Infographic: plan}}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/articles/"+id+"/infographic.svg", nil))
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/svg+xml") || strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("invalid SVG response: %d %s", w.Code, w.Body.String())
	}
}

func TestInfographicButtonAppearsWithSummary(t *testing.T) {
	a := testApp(t)
	id := strings.Repeat("c", 64)
	a.db = &infographicTestDB{article: store.Article{ID: id, URL: "https://example.com", Title: "記事", Status: "ready", Points: []string{"要点"}}}
	r := httptest.NewRequest("GET", "/articles/"+id, nil)
	r.AddCookie(&http.Cookie{Name: "shiori_session", Value: "token"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "絵で説明する") || !strings.Contains(w.Body.String(), "infographic-panel") {
		t.Fatalf("missing infographic control: %d %s", w.Code, w.Body.String())
	}
}

func TestCompletedInfographicCanBeRemade(t *testing.T) {
	a := testApp(t)
	id := strings.Repeat("d", 64)
	a.db = &infographicTestDB{article: store.Article{ID: id, URL: "https://example.com", Title: "記事", Status: "ready", Points: []string{"要点"}, InfographicStatus: "ready"}}
	r := httptest.NewRequest("GET", "/articles/"+id, nil)
	r.AddCookie(&http.Cookie{Name: "shiori_session", Value: "token"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "絵を作り直す") {
		t.Fatalf("completed visual cannot be remade: %d %s", w.Code, w.Body.String())
	}
}

func TestGeneratedInfographicServedAsImage(t *testing.T) {
	a := testApp(t)
	id := strings.Repeat("e", 64)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}
	picture, err := infographic.PrepareImage(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	a.db = &infographicTestDB{article: store.Article{ID: id, InfographicStatus: "ready", InfographicImageKey: "current"}, picture: picture}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/articles/"+id+"/infographic/image", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), picture.Data) || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("invalid image response: %d %v", w.Code, w.Header())
	}
}
