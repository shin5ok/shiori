package store

import (
	"errors"
	"reflect"
	"testing"

	"my-bookmark/internal/summary"
)

func TestAutomaticTagsPreserveManualTagsAndReuse(t *testing.T) {
	s, ctx := ratingTestStore(t)
	s.tagCandidates = []string{"AI", "技術", "OSS", "旅行", "歴史"}
	u, v, editor := User{ID: "u"}, User{ID: "v"}, User{ID: "editor"}
	raw := "https://example.com/" + RandomToken()
	a, err := s.Save(ctx, u, raw, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, v, raw, "", []string{"趣味"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, editor, raw, "", nil); err != nil {
		t.Fatal(err)
	}
	_, lease, err := s.NextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A manual edit while generation is running must win.
	if _, err := s.Save(ctx, editor, raw, "", []string{"編集済み"}); err != nil {
		t.Fatal(err)
	}
	tags := []string{"AI", "技術", "OSS"}
	if err := s.FinishJobWithTags(ctx, a.ID, "stale", "記事", []string{"要点"}, "test", nil, "", nil, tags); !errors.Is(err, ErrBusy) {
		t.Fatalf("stale result: %v", err)
	}
	if err := s.FinishJobWithTags(ctx, a.ID, lease, "記事", []string{"要点"}, "test", nil, "", nil, tags); err != nil {
		t.Fatal(err)
	}
	for uid, want := range map[string][]string{"u": tags, "v": {"趣味"}, "editor": {"編集済み"}} {
		b, err := s.Own(ctx, uid, a.ID)
		if err != nil || !reflect.DeepEqual(b.Tags, want) {
			t.Fatalf("%s tags: %+v %v", uid, b, err)
		}
	}
	if _, err := s.Save(ctx, User{ID: "new"}, raw, "", nil); err != nil {
		t.Fatal(err)
	}
	b, err := s.Own(ctx, "new", a.ID)
	if err != nil || !reflect.DeepEqual(b.Tags, tags) {
		t.Fatalf("cached tags: %+v %v", b, err)
	}
	if _, err := s.Enqueue(ctx, u.ID, a.ID, summary.Detailed, ""); err != nil {
		t.Fatal(err)
	}
	_, lease, err = s.NextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJobWithTags(ctx, a.ID, lease, "記事", nil, "test", nil, "failed", nil, []string{"旅行", "歴史"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil || !reflect.DeepEqual(got.SuggestedTags, tags) {
		t.Fatalf("failed job changed tags: %+v %v", got, err)
	}
}

func TestReadyArticleWithoutTagsQueuesClassification(t *testing.T) {
	s, ctx := ratingTestStore(t)
	s.tagCandidates = []string{"AI", "技術", "OSS", "旅行", "歴史"}
	u := User{ID: "reader"}
	a, err := s.Save(ctx, u, "https://example.com/"+RandomToken(), "", []string{"手動"})
	if err != nil {
		t.Fatal(err)
	}
	_, lease, err := s.NextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, a.ID, lease, "記事", []string{"要点"}, "test", nil, "", nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Save(ctx, User{ID: "new"}, a.URL, "", nil)
	if err != nil || got.Status != "queued" {
		t.Fatalf("legacy classification: %+v %v", got, err)
	}
}
