package app

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"my-bookmark/internal/content"
	"my-bookmark/internal/store"
)

type Page struct {
	TokenSettings, TokenActive bool
	AccountSettings            bool
	TagList                    []store.TagCount
	Tag, Return                string
	TagBase, ClearTag          string
	Accounts                   *AccountsPage
	APIEndpoint, IssuedToken   string
	TokenExpires               time.Time
	Title, Mode, Next, Error   string
	Session                    *store.Session
	Entries                    []store.Entry
	Comments                   []store.Bookmark
	Detail                     bool
	Status                     int
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, p Page) {
	p.Session = session(r)
	p.Status = status
	var b bytes.Buffer
	if err := a.templates.ExecuteTemplate(&b, "page", p); err != nil {
		http.Error(w, "ページを表示できません", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(b.Bytes())
}
func (a *App) problem(w http.ResponseWriter, r *http.Request, status int, message string) {
	a.render(w, r, status, Page{Title: "操作を確認してください", Error: message})
}
func (a *App) feed(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		query := r.URL.Query()
		if sort := query.Get("sort"); sort == "new" || sort == "popular" {
			query.Del("sort")
			target := "/" + sort
			if encoded := query.Encode(); encoded != "" {
				target += "?" + encoded
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
	}
	mode := "new"
	title := "新着ブックマーク"
	if r.URL.Path == "/popular" {
		mode = "popular"
		title = "人気のブックマーク"
	}
	if r.URL.Path == "/mine" || r.URL.Path == "/" {
		mode = "mine"
		title = "マイブックマーク"
		if r.URL.Query().Get("filter") == "unread" {
			mode = "unread"
			title = "まだ理解していないブックマーク"
		}
		if session(r) == nil {
			http.Redirect(w, r, "/auth/google", 302)
			return
		}
	}
	uid := ""
	if s := session(r); s != nil {
		uid = s.User.ID
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" && !validID.MatchString(cursor) {
		a.problem(w, r, 400, "ページの指定が正しくありません。")
		return
	}
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	if tag != "" {
		if mode != "mine" && mode != "unread" || utf8.RuneCountInString(tag) > 24 || strings.ContainsAny(tag, ",\r\n") {
			a.problem(w, r, 400, "タグの指定が正しくありません。")
			return
		}
		title = "タグ「" + tag + "」のブックマーク"
	}
	var entries []store.Entry
	var next string
	var err error
	if tag != "" {
		entries, next, err = a.db.ListByTag(r.Context(), mode, uid, tag, cursor)
	} else {
		entries, next, err = a.db.List(r.Context(), mode, uid, cursor)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var tagList []store.TagCount
	if uid != "" {
		if tagList, err = a.db.Tags(r.Context(), uid, 40); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	// Tag links keep the unread filter; the selected tag links back to the unfiltered list.
	tagBase, clearTag := "/?", "/"
	if mode == "unread" {
		tagBase, clearTag = "/?filter=unread&", "/?filter=unread"
	}
	nextURL := ""
	if next != "" {
		if mode == "mine" || mode == "unread" {
			v := url.Values{"cursor": {next}}
			if mode == "unread" {
				v.Set("filter", "unread")
			}
			if tag != "" {
				v.Set("tag", tag)
			}
			nextURL = "/?" + v.Encode()
		} else {
			nextURL = "/" + mode + "?cursor=" + next
		}
	}
	a.render(w, r, 200, Page{Title: title, Mode: mode, Entries: entries, Next: nextURL, TagList: tagList, Tag: tag, Return: r.URL.RequestURI(), TagBase: tagBase, ClearTag: clearTag})
}
func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	article, err := a.db.Get(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	entry := store.Entry{Article: article}
	if s := session(r); s != nil {
		entry.Bookmark, err = a.db.Own(r.Context(), s.User.ID, id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			a.fail(w, r, err)
			return
		}
	}
	comments, err := a.db.Comments(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, 200, Page{Title: article.Title, Detail: true, Entries: []store.Entry{entry}, Comments: comments})
}
func bookmarkInput(v url.Values) (string, string, []string, error) {
	raw, err := content.NormalizeURL(v.Get("url"))
	if err != nil {
		return "", "", nil, err
	}
	comment := strings.TrimSpace(v.Get("comment"))
	if utf8.RuneCountInString(comment) > 500 {
		return "", "", nil, errors.New("コメントは500文字以内で入力してください")
	}
	var tags []string
	seen := map[string]bool{}
	for _, tag := range strings.Split(v.Get("tags"), ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		if utf8.RuneCountInString(tag) > 24 {
			return "", "", nil, errors.New("タグは1つ24文字以内で入力してください")
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	if len(tags) > 5 {
		return "", "", nil, errors.New("タグは5つまで指定できます")
	}
	return raw, comment, tags, nil
}
func (a *App) save(w http.ResponseWriter, r *http.Request) {
	raw, comment, tags, err := bookmarkInput(r.PostForm)
	if err != nil {
		a.problem(w, r, 400, err.Error())
		return
	}
	article, err := a.db.Save(r.Context(), session(r).User, raw, comment, tags)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/articles/"+article.ID, http.StatusSeeOther)
}
func (a *App) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	if err := a.db.Delete(r.Context(), session(r).User.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, returnPath(r.PostFormValue("return")), http.StatusSeeOther)
}

// returnPath accepts only same-site paths so a form cannot redirect elsewhere.
func returnPath(v string) string {
	if v == "" || len(v) > 300 || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsAny(v, "\\\r\n") {
		return "/mine"
	}
	return v
}
