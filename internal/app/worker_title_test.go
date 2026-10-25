package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"my-bookmark/internal/store"
	"my-bookmark/internal/summary"
)

type titleJobQueue struct {
	article store.Article
	title   string
	failure string
	tldr    []string
}

func (q *titleJobQueue) NextQueued(context.Context) (*store.SummaryJob, string, error) {
	return nil, "", nil
}
func (q *titleJobQueue) Get(context.Context, string) (store.Article, error) { return q.article, nil }
func (q *titleJobQueue) UpdateStage(context.Context, string, string, string, string, []string) error {
	return nil
}
func (q *titleJobQueue) FinishJob(_ context.Context, _, _, title string, _ []string, _ string, _ []string, failure string, tldr []string) error {
	q.title, q.failure, q.tldr = title, failure, tldr
	return nil
}

type titleSummarizer struct{ result summary.Result }

func (s titleSummarizer) Summarize(context.Context, string, string, summary.Style, string) (summary.Result, error) {
	return s.result, nil
}

func TestWorkerPrefersThePageTitle(t *testing.T) {
	for _, tc := range []struct {
		name        string
		style       summary.Style
		instruction string
		pageTitle   string
		generated   string
		want        string
	}{
		{"page title beats generated", summary.Standard, "", "元の記事タイトル", "新しい日本語タイトル", "元の記事タイトル"},
		{"page title beats generated when restyled", summary.Concrete, "", "元の記事タイトル", "新しい日本語タイトル", "元の記事タイトル"},
		{"page title without generated", summary.Standard, "", "元の記事タイトル", "", "元の記事タイトル"},
		{"generated when the page has no title", summary.Standard, "", "", "新しい日本語タイトル", "新しい日本語タイトル"},
		{"previous title kept when restyled without any title", summary.Concrete, "", "", "", "以前のタイトル"},
		{"custom instruction may retitle", summary.Standard, "英語で書いて", "元の記事タイトル", "New English title", "New English title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &titleJobQueue{article: store.Article{URL: "https://example.com/article", Title: "以前のタイトル", Active: true, Count: 1}}
			w := NewWorker(q, titleSummarizer{summary.Result{Title: tc.generated, TLDR: []string{"結論", "重要性", "影響"}, Points: []string{"記事の結論"}}}, "test")
			w.fetcher = &http.Client{Transport: articleTransportWithTitle{title: tc.pageTitle}}
			w.process(context.Background(), "article", "lease", tc.style, tc.instruction)
			if len(q.tldr) != 3 {
				t.Fatal("TLDR was not forwarded")
			}
			if q.failure != "" || q.title != tc.want {
				t.Fatalf("title=%q failure=%q; want %q", q.title, q.failure, tc.want)
			}
		})
	}
}

type articleTransportWithTitle struct{ title string }

func (a articleTransportWithTitle) RoundTrip(*http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	head := ""
	if a.title != "" {
		head = "<title>" + a.title + "</title>"
	}
	w.WriteString(`<html>` + head + `<article><p>` + strings.Repeat("記事の本文です。", 200) + `</p></article></html>`)
	return w.Result(), nil
}
