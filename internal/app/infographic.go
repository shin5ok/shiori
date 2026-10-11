package app

import (
	"errors"
	"net/http"
	"time"

	"my-bookmark/internal/infographic"
	"my-bookmark/internal/store"
)

func infographicStatus(a store.Article) string {
	if a.InfographicStatus == "processing" && !a.InfographicLeaseUntil.After(time.Now()) {
		return "interrupted"
	}
	return a.InfographicStatus
}

func (a *App) generateInfographic(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	err := a.db.EnqueueInfographic(r.Context(), session(r).User.ID, id)
	if errors.Is(err, store.ErrRateLimited) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "利用上限です（1分3件・1日50件）。時間をおいて再試行してください。"})
		return
	}
	if errors.Is(err, store.ErrNotReady) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "要約が完成してから図解を作成してください。"})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		a.problem(w, r, 404, "ブックマークが見つかりません。")
		return
	}
	if err != nil && !errors.Is(err, store.ErrBusy) {
		a.fail(w, r, err)
		return
	}
	article, err := a.db.Get(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, summaryData(article))
}

func (a *App) infographicImage(w http.ResponseWriter, r *http.Request) {
	id, ok := a.id(w, r)
	if !ok {
		return
	}
	article, err := a.db.Get(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if (article.Infographic == nil && article.InfographicImageKey == "") || article.InfographicStatus != "ready" {
		http.NotFound(w, r)
		return
	}
	if article.InfographicImageKey != "" {
		picture, err := a.db.GetInfographicImage(r.Context(), id, article.InfographicImageKey)
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			a.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", picture.MIME)
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(picture.Data)
		return
	}
	svg, err := infographic.Render(*article.Infographic)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(svg)
}
