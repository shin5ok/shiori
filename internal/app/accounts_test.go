package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"my-bookmark/internal/iap"
	"my-bookmark/internal/store"
)

type linkDB struct {
	testDB
	sessions map[string]store.Session
	links    map[string]store.AccountLink
	invites  map[string]store.AccountInvite
	listed   string
}

func newLinkDB() *linkDB {
	return &linkDB{sessions: map[string]store.Session{}, links: map[string]store.AccountLink{}, invites: map[string]store.AccountInvite{}}
}
func (d *linkDB) Session(_ context.Context, token string) (store.Session, error) {
	if s, ok := d.sessions[token]; ok {
		return s, nil
	}
	return store.Session{}, store.ErrNotFound
}
func (d *linkDB) PutSession(_ context.Context, token string, s store.Session) error {
	d.sessions[token] = s
	return nil
}
func (d *linkDB) List(_ context.Context, _, uid, _ string) ([]store.Entry, string, error) {
	d.listed = uid
	return nil, "", nil
}
func (d *linkDB) Link(_ context.Context, id string) (store.AccountLink, error) {
	if l, ok := d.links[id]; ok {
		return l, nil
	}
	return store.AccountLink{}, store.ErrNotFound
}
func (d *linkDB) UserName(context.Context, string) (string, error) { return "Base", nil }
func (d *linkDB) HasLinkedEmail(_ context.Context, base, email string) (bool, error) {
	for _, l := range d.links {
		if l.BaseID == base && l.Email == email {
			return true, nil
		}
	}
	return false, nil
}
func (d *linkDB) HasPendingInvite(_ context.Context, email string) (bool, error) {
	for _, i := range d.invites {
		if i.Email == email && i.ExpiresAt.After(time.Now()) {
			return true, nil
		}
	}
	return false, nil
}
func (d *linkDB) LinkedAccounts(context.Context, string) ([]store.AccountLink, error) {
	return nil, nil
}
func (d *linkDB) PendingInvites(context.Context, string) ([]store.AccountInvite, error) {
	return nil, nil
}
func (d *linkDB) ReapExpiredInvites(context.Context, string) ([]string, error) { return nil, nil }
func (d *linkDB) CreateInvite(_ context.Context, base, email, token string, ttl time.Duration) error {
	d.invites[token] = store.AccountInvite{BaseID: base, Email: email, ExpiresAt: time.Now().Add(ttl)}
	return nil
}
func (d *linkDB) DeleteInvite(_ context.Context, token string) error {
	delete(d.invites, token)
	return nil
}
func (d *linkDB) Invite(_ context.Context, token string) (store.AccountInvite, error) {
	if i, ok := d.invites[token]; ok {
		return i, nil
	}
	return store.AccountInvite{}, store.ErrNotFound
}
func (d *linkDB) ConfirmInvite(_ context.Context, token, login, email string) (store.AccountLink, error) {
	i, ok := d.invites[token]
	if !ok {
		return store.AccountLink{}, store.ErrNotFound
	}
	if i.Email != email {
		return store.AccountLink{}, store.ErrEmailMismatch
	}
	delete(d.invites, token)
	d.links[login] = store.AccountLink{LoginID: login, BaseID: i.BaseID, Email: i.Email}
	return d.links[login], nil
}
func (d *linkDB) Unlink(_ context.Context, base, login string) (store.AccountLink, error) {
	l, ok := d.links[login]
	if !ok || l.BaseID != base {
		return store.AccountLink{}, store.ErrNotFound
	}
	delete(d.links, login)
	return l, nil
}
func (d *linkDB) EmailInUse(_ context.Context, email string) (bool, error) {
	for _, l := range d.links {
		if l.Email == email {
			return true, nil
		}
	}
	return false, nil
}

type captureMailer struct{ to, body string }

func (m *captureMailer) Send(_ context.Context, to, _, body string) error {
	m.to, m.body = to, body
	return nil
}

type recordGranter struct{ granted, revoked []string }

func (g *recordGranter) Grant(_ context.Context, e string) error {
	g.granted = append(g.granted, e)
	return nil
}
func (g *recordGranter) Revoke(_ context.Context, e string) error {
	g.revoked = append(g.revoked, e)
	return nil
}

type linkClient struct {
	app     *App
	cookies map[string]*http.Cookie
	email   string
	subject string
}

func (c *linkClient) do(t *testing.T, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", c.app.cfg.BaseURL)
	}
	r.Header.Set("X-Goog-IAP-JWT-Assertion", "signed")
	for _, ck := range c.cookies {
		r.AddCookie(ck)
	}
	c.app.iapVerifier = iapTestVerifier{iap.Identity{Email: c.email, Subject: c.subject}}
	w := httptest.NewRecorder()
	c.app.Handler().ServeHTTP(w, r)
	for _, ck := range w.Result().Cookies() {
		c.cookies[ck.Name] = ck
	}
	return w
}

var csrfPattern = regexp.MustCompile(`name="csrf-token" content="([^"]+)"`)

func (c *linkClient) csrf(t *testing.T) string {
	t.Helper()
	w := c.do(t, "GET", "/settings/accounts", nil)
	m := csrfPattern.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("no csrf token: %d %s", w.Code, w.Body.String())
	}
	return m[1]
}

func TestAccountLinkingSharesBaseAccount(t *testing.T) {
	list, err := iap.ParseAllowlist([]byte("domains: [data-cloud.jp]"))
	if err != nil {
		t.Fatal(err)
	}
	db := newLinkDB()
	a, err := New(Config{Env: "production", BaseURL: "https://bookmark.example", IAPAudience: "/projects/123/locations/asia-northeast1/services/shiori", IAPAllowlist: list}, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	mailer, granter := &captureMailer{}, &recordGranter{}
	a.SetLinking(mailer, granter)
	base := &linkClient{app: a, cookies: map[string]*http.Cookie{}, email: "shingo@data-cloud.jp", subject: "base-sub"}
	guest := &linkClient{app: a, cookies: map[string]*http.Cookie{}, email: "guest@example.com", subject: "guest-sub"}
	baseID := store.Hash("https://accounts.google.com|base-sub")
	guestID := store.Hash("https://accounts.google.com|guest-sub")

	if w := guest.do(t, "GET", "/", nil); w.Code != http.StatusForbidden {
		t.Fatalf("unlinked outsider status=%d", w.Code)
	}
	token := base.csrf(t)
	w := base.do(t, "POST", "/settings/accounts/invite", url.Values{"csrf": {token}, "email": {"Guest@Example.com"}})
	if w.Code != http.StatusOK || mailer.to != "guest@example.com" || len(granter.granted) != 1 || granter.granted[0] != "guest@example.com" {
		t.Fatalf("invite: status=%d mail=%q grants=%v", w.Code, mailer.to, granter.granted)
	}
	link := regexp.MustCompile(`/accounts/link\?token=[^\s]+`).FindString(mailer.body)
	if link == "" {
		t.Fatalf("mail has no link: %s", mailer.body)
	}
	// Opening the link must not link by itself (mail scanners prefetch).
	if w = guest.do(t, "GET", link, nil); w.Code != http.StatusOK || len(db.links) != 0 {
		t.Fatalf("confirm page status=%d links=%d", w.Code, len(db.links))
	}
	// Another signed-in account cannot use the link.
	other := &linkClient{app: a, cookies: map[string]*http.Cookie{}, email: "kawano@data-cloud.jp", subject: "other-sub"}
	if w = other.do(t, "GET", link, nil); w.Code != http.StatusForbidden {
		t.Fatalf("other account status=%d", w.Code)
	}
	guestToken := csrfFrom(t, guest.do(t, "GET", link, nil))
	query, _ := url.ParseQuery(strings.TrimPrefix(link, "/accounts/link?"))
	if w = guest.do(t, "POST", "/accounts/link", url.Values{"csrf": {guestToken}, "token": {query.Get("token")}}); w.Code != http.StatusSeeOther {
		t.Fatalf("confirm status=%d %s", w.Code, w.Body.String())
	}
	// The session issued before linking must now resolve to the base account.
	if w = guest.do(t, "GET", "/", nil); w.Code != http.StatusOK || db.listed != baseID {
		t.Fatalf("linked login uses %q, want base %q (status %d)", db.listed, baseID, w.Code)
	}
	if db.links[guestID].BaseID != baseID {
		t.Fatalf("link not recorded: %#v", db.links)
	}
	sessions := len(db.sessions)
	guest.do(t, "GET", "/", nil)
	if len(db.sessions) != sessions {
		t.Fatal("linked login creates a new session on every request")
	}
	// Linked accounts cannot manage links; unlink revokes access immediately.
	if w = guest.do(t, "GET", "/settings/accounts", nil); !strings.Contains(w.Body.String(), "ベースアカウントでログイン") {
		t.Fatalf("linked account sees management UI: %s", w.Body.String())
	}
	if w = base.do(t, "POST", "/settings/accounts/unlink", url.Values{"csrf": {token}, "login": {guestID}}); w.Code != http.StatusSeeOther {
		t.Fatalf("unlink status=%d", w.Code)
	}
	if len(granter.revoked) != 1 || granter.revoked[0] != "guest@example.com" {
		t.Fatalf("revoked=%v", granter.revoked)
	}
	if w = guest.do(t, "GET", "/", nil); w.Code != http.StatusForbidden {
		t.Fatalf("after unlink status=%d", w.Code)
	}
}

func csrfFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	m := csrfPattern.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("no csrf token in %d", w.Code)
	}
	return m[1]
}

func TestInviteRejectsInvalidEmail(t *testing.T) {
	list, _ := iap.ParseAllowlist([]byte("domains: [data-cloud.jp]"))
	db := newLinkDB()
	a, err := New(Config{Env: "production", BaseURL: "https://bookmark.example", IAPAudience: "/projects/123/locations/asia-northeast1/services/shiori", IAPAllowlist: list}, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.SetLinking(&captureMailer{}, &recordGranter{})
	base := &linkClient{app: a, cookies: map[string]*http.Cookie{}, email: "shingo@data-cloud.jp", subject: "base-sub"}
	token := base.csrf(t)
	for _, email := range []string{"", "not-an-email", "shingo@data-cloud.jp", "a@b.jp\r\nBcc: x@y.jp"} {
		if w := base.do(t, "POST", "/settings/accounts/invite", url.Values{"csrf": {token}, "email": {email}}); w.Code != http.StatusBadRequest {
			t.Errorf("email %q: status=%d", email, w.Code)
		}
	}
	if len(db.invites) != 0 {
		t.Fatal("invalid invite stored")
	}
}
