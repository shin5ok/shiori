package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"my-bookmark/internal/content"
	"my-bookmark/internal/store"
)

var apiURLCandidate = regexp.MustCompile(`(?i)https?://[^\s\p{Z}<>"'「」『』【】\[\]（）。、！？；：]+`)

// extractSingleAPIURL accepts surrounding prose while keeping ambiguous input out of Save.
// The extracted URL still goes through the same validation as browser submissions.
func extractSingleAPIURL(input string) (string, error) {
	candidates := apiURLCandidate.FindAllString(input, -1)
	if len(candidates) != 1 {
		return "", errors.New("URLは1件だけ指定してください")
	}
	candidate := strings.TrimRight(candidates[0], ".,!?;:。、！？；：")
	for len(candidate) > 0 {
		end, size := utf8.DecodeLastRuneInString(candidate)
		if !strings.ContainsRune(")]}）」", end) {
			break
		}
		if end == ')' && strings.Count(candidate, "(") >= strings.Count(candidate, ")") {
			break
		}
		candidate = candidate[:len(candidate)-size]
	}
	return content.NormalizeURL(candidate)
}

func apiJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (a *App) apiError(w http.ResponseWriter, r *http.Request, status int, message string) {
	a.apiResponse(w, r, status, map[string]string{"error": message})
}

func (a *App) apiTokenError(w http.ResponseWriter, r *http.Request, code, message string) {
	a.apiResponse(w, r, http.StatusUnauthorized, map[string]string{"error": "invalid token", "code": code, "message": message})
}

// Prefer HTML only when explicitly accepted; wildcard-only clients keep JSON.
func acceptsAPIHTML(r *http.Request) bool {
	htmlQuality, jsonQuality := -1.0, -1.0
	for _, header := range r.Header.Values("Accept") {
		for _, item := range strings.Split(header, ",") {
			media, params, err := mime.ParseMediaType(strings.TrimSpace(item))
			if err != nil {
				continue
			}
			quality := 1.0
			if raw, ok := params["q"]; ok {
				quality, err = strconv.ParseFloat(raw, 64)
				if err != nil || !(quality >= 0 && quality <= 1) {
					continue
				}
			}
			switch media {
			case "text/html":
				htmlQuality = max(htmlQuality, quality)
			case "application/json":
				jsonQuality = max(jsonQuality, quality)
			}
		}
	}
	return htmlQuality > 0 && htmlQuality >= jsonQuality
}

func (a *App) apiResponse(w http.ResponseWriter, r *http.Request, status int, result map[string]string) {
	w.Header().Add("Vary", "Accept")
	if !acceptsAPIHTML(r) {
		apiJSON(w, status, result)
		return
	}
	nonce := store.RandomToken()
	policy := w.Header().Get("Content-Security-Policy")
	w.Header().Set("Content-Security-Policy", strings.Replace(policy, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	if err := a.templates.ExecuteTemplate(w, "api-result", struct {
		Result         map[string]string
		HomeURL, Nonce string
		Success        bool
	}{result, a.cfg.BaseURL + "/", nonce, status < 400}); err != nil {
		slog.Error("render API result", "error", err)
	}
}

func (a *App) saveAPI(w http.ResponseWriter, r *http.Request) {
	// API authentication never falls back to browser cookies or IAP headers.
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		a.apiError(w, r, 400, "invalid query")
		return
	}
	values := q["token"]
	headers := r.Header.Values("X-Shiori-Api")
	if (len(values) > 0 && len(headers) > 0) || len(values) > 1 || len(headers) > 1 || (len(headers) == 1 && strings.Contains(headers[0], ",")) {
		a.apiTokenError(w, r, "token_ambiguous", "URLのクエリパラメータ token または x-shiori-api ヘッダーのどちらか一方に、トークンを1つだけ指定してください。")
		return
	}
	if len(headers) > 0 {
		values = headers
	}
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		a.apiTokenError(w, r, "token_missing", "URLのクエリパラメータ token または x-shiori-api ヘッダーに、発行したトークンを指定してください。")
		return
	}
	// Tokens use only URL-safe ASCII; surrounding clipboard whitespace is not part of the secret.
	rawToken := strings.TrimSpace(values[0])
	uid, secret, ok := strings.Cut(rawToken, ".")
	if !ok || !validID.MatchString(uid) || len(secret) != 43 {
		a.apiTokenError(w, r, "token_malformed", "トークンは108文字（64文字のID・ドット・43文字の秘密値）です。発行画面から全体をコピーし、前後に空白や改行を含めないでください。")
		return
	}
	token, err := a.db.APIToken(r.Context(), uid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.apiError(w, r, 503, "token lookup failed")
		return
	}
	if err != nil || token.User.ID != uid || subtle.ConstantTimeCompare([]byte(token.Hash), []byte(store.Hash(rawToken))) != 1 {
		a.apiTokenError(w, r, "token_invalid", "現在のトークンと一致しません。発行元のAPIエンドポイントを確認し、再発行した最新のトークンを使用してください。再発行前や失効済みのトークンは使えません。")
		return
	}
	if !token.ExpiresAt.After(time.Now()) {
		a.apiTokenError(w, r, "token_expired", "トークンの有効期限が切れています。ログインしてAPIトークンを再発行してください。")
		return
	}
	if a.cfg.Env == "production" && !a.cfg.IAPAllowlist.Allows(token.Email) {
		a.apiError(w, r, 403, "account is not allowed")
		return
	}
	var input struct {
		URL     string   `json:"url"`
		Comment string   `json:"comment"`
		Tags    []string `json:"tags"`
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		a.apiError(w, r, 415, "Content-Type must be application/json")
		return
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		a.apiError(w, r, 400, "invalid JSON")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		a.apiError(w, r, 400, "expected one JSON object")
		return
	}
	for _, tag := range input.Tags {
		if strings.Contains(tag, ",") {
			a.apiError(w, r, 400, "tags cannot contain commas")
			return
		}
	}
	extracted, err := extractSingleAPIURL(input.URL)
	if err != nil {
		a.apiError(w, r, 400, err.Error())
		return
	}
	raw, comment, tags, err := bookmarkInput(url.Values{"url": {extracted}, "comment": {input.Comment}, "tags": {strings.Join(input.Tags, ",")}})
	if err != nil {
		a.apiError(w, r, 400, err.Error())
		return
	}
	article, err := a.db.Save(r.Context(), token.User, raw, comment, tags)
	if err != nil {
		if errors.Is(err, store.ErrRateLimited) {
			a.apiError(w, r, 429, "quota exceeded")
			return
		}
		a.apiError(w, r, 503, "save failed")
		return
	}
	a.apiResponse(w, r, 200, map[string]string{"id": article.ID, "url": article.URL, "title": article.Title, "status": article.Status, "article_url": a.cfg.BaseURL + "/articles/" + article.ID, "redirect_url": a.cfg.BaseURL + "/"})
}

func (a *App) tokenSettings(w http.ResponseWriter, r *http.Request) {
	a.renderTokenSettings(w, r, "")
}
func (a *App) renderTokenSettings(w http.ResponseWriter, r *http.Request, raw string) {
	token, err := a.db.APIToken(r.Context(), session(r).User.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.fail(w, r, err)
		return
	}
	endpoint := a.cfg.APIBaseURL
	if endpoint == "" && a.cfg.Env == "development" {
		endpoint = a.cfg.BaseURL
	}
	if endpoint != "" {
		endpoint += "/api/bookmarks"
	}
	// no-referrer makes browser form POSTs send Origin: null, failing CSRF.
	// Keep the origin for same-origin forms without sharing referrers externally.
	w.Header().Set("Referrer-Policy", "same-origin")
	a.render(w, r, 200, Page{Title: "APIトークン", TokenSettings: true, APIEndpoint: endpoint, IssuedToken: raw, TokenExpires: token.ExpiresAt, TokenActive: err == nil && token.ExpiresAt.After(time.Now())})
}
func (a *App) issueToken(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	if !validID.MatchString(s.User.ID) {
		a.problem(w, r, 400, "ユーザー情報を確認できません。")
		return
	}
	if a.cfg.Env == "production" && !a.cfg.IAPAllowlist.Allows(s.Email) {
		a.problem(w, r, 403, "このアカウントにはアクセス権がありません。")
		return
	}
	raw := s.User.ID + "." + store.RandomToken()
	token := store.APIToken{User: s.User, Email: s.Email, Hash: store.Hash(raw), ExpiresAt: time.Now().Add(90 * 24 * time.Hour)}
	if err := a.db.PutAPIToken(r.Context(), token); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderTokenSettings(w, r, raw)
}
func (a *App) revokeToken(w http.ResponseWriter, r *http.Request) {
	if err := a.db.DeleteAPIToken(r.Context(), session(r).User.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings/api", http.StatusSeeOther)
}
