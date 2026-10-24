package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"my-bookmark/internal/store"
)

type tagViewDB struct {
	testDB
	tag, deleted string
}

func (d *tagViewDB) Tags(context.Context, string, int) ([]store.TagCount, error) {
	return []store.TagCount{{Name: "技術", Count: 3}, {Name: "旅行", Count: 1}}, nil
}
func (d *tagViewDB) ListByTag(_ context.Context, _, _, tag, _ string) ([]store.Entry, string, error) {
	d.tag = tag
	return []store.Entry{{Article: store.Article{ID: strings.Repeat("a", 64), Title: "記事", Status: "ready"}, Bookmark: &store.Bookmark{Tags: []string{tag}}}}, strings.Repeat("b", 64), nil
}
func (d *tagViewDB) Delete(_ context.Context, _, id string) error { d.deleted = id; return nil }

func TestTagSidebarFilterAndViewToggle(t *testing.T) {
	a := testApp(t)
	db := &tagViewDB{}
	a.db = db
	r := httptest.NewRequest(http.MethodGet, "/?tag="+url.QueryEscape("技術"), nil)
	r.AddCookie(&http.Cookie{Name: "shiori_session", Value: "token"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != http.StatusOK || db.tag != "技術" {
		t.Fatalf("status=%d tag=%q", w.Code, db.tag)
	}
	for _, want := range []string{
		`class="tag-nav"`,
		`href="/?tag=%e6%8a%80%e8%a1%93" aria-current="page"`,
		`href="/?tag=%e6%97%85%e8%a1%8c"`,
		`href="/?cursor=` + strings.Repeat("b", 64) + `&amp;tag=%E6%8A%80%E8%A1%93"`,
		`data-view="compact"`,
		`class="delete-button"`,
		`name="return" value="/?tag=%E6%8A%80%E8%A1%93"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestTagFilterRejectsInvalidTagAndPublicFeeds(t *testing.T) {
	for _, path := range []string{"/new?tag=x", "/?tag=" + strings.Repeat("a", 25)} {
		a := testApp(t)
		a.db = &tagViewDB{}
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "shiori_session", Value: "token"})
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d", path, w.Code)
		}
	}
}

func TestReturnPathOnlyAllowsSamePathRedirects(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "/mine",
		"/?tag=x":                      "/?tag=x",
		"//evil.example":               "/mine",
		"https://evil.example":         "/mine",
		"/\\evil.example":              "/mine",
		"/ok\r\nLocation: x":           "/mine",
		"/" + strings.Repeat("a", 400): "/mine",
	} {
		if got := returnPath(in); got != want {
			t.Errorf("returnPath(%q)=%q want %q", in, got, want)
		}
	}
}
