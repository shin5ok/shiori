package store

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"my-bookmark/internal/infographic"
	"my-bookmark/internal/summary"
)

func TestInfographicJobLifecycleAndSummaryInvalidation(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("requires Firestore Emulator")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := firestore.NewClient(ctx, "demo-visual-"+Hash(RandomToken())[:12])
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	u := User{ID: Hash(RandomToken()), Name: "reader"}
	v := User{ID: Hash(RandomToken()), Name: "second reader"}
	raw := "https://example.com/" + RandomToken()
	a, err := s.Save(ctx, u, raw, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, v, raw, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueInfographic(ctx, u.ID, a.ID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("accepted unfinished summary: %v", err)
	}
	_, summaryLease, err := s.NextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, a.ID, summaryLease, "記事", []string{"要点"}, "test", nil, "", []string{"結論"}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueInfographic(ctx, "other", a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unowned request: %v", err)
	}
	if err := s.EnqueueInfographic(ctx, u.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueInfographic(ctx, u.ID, a.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("duplicate request: %v", err)
	}
	job, lease, err := s.NextInfographic(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim: %+v %v", job, err)
	}
	plan := &infographic.Plan{Title: "図解", Takeaway: "結論", Kind: "flow", Items: []infographic.Item{{Heading: "一", Detail: "説明"}, {Heading: "二", Detail: "説明"}, {Heading: "三", Detail: "説明"}}}
	if err := s.FinishInfographic(ctx, a.ID, "wrong", plan, ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("stale lease: %v", err)
	}
	if err := s.FinishInfographic(ctx, a.ID, lease, plan, ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil || got.InfographicStatus != "ready" || got.Infographic == nil || got.Infographic.Title != "図解" {
		t.Fatalf("saved image: %+v %v", got, err)
	}
	if err := s.EnqueueInfographic(ctx, v.ID, a.ID); err != nil {
		t.Fatalf("cannot remake completed visual: %v", err)
	}
	previousLease := lease
	_, lease, err = s.NextInfographic(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}
	picture, err := infographic.PrepareImage(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishInfographicImage(ctx, a.ID, previousLease, &picture, ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("stale image lease accepted: %v", err)
	}
	if err := s.FinishInfographicImage(ctx, a.ID, lease, &picture, ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(ctx, a.ID)
	if err != nil || got.Infographic != nil || got.InfographicImageKey == "" || got.InfographicStatus != "ready" {
		t.Fatalf("image not published: %+v %v", got, err)
	}
	imageKey := got.InfographicImageKey
	saved, err := s.GetInfographicImage(ctx, a.ID, imageKey)
	if err != nil || !bytes.Equal(saved.Data, picture.Data) {
		t.Fatalf("image not saved separately: %v", err)
	}
	if _, err := s.GetInfographicImage(ctx, a.ID, "old-key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("served mismatched image: %v", err)
	}
	if _, err := s.Enqueue(ctx, u.ID, a.ID, summary.Simple, ""); err != nil {
		t.Fatal(err)
	}
	_, summaryLease, err = s.NextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, a.ID, summaryLease, "更新記事", []string{"新要点"}, "test", nil, "", []string{"新結論"}); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(ctx, a.ID)
	if err != nil || got.Infographic != nil || got.InfographicStatus != "" || got.InfographicImageKey != "" {
		t.Fatalf("old image retained: %+v %v", got, err)
	}
	if _, err := s.GetInfographicImage(ctx, a.ID, imageKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("obsolete image data retained: %v", err)
	}
	if err := s.EnqueueInfographic(ctx, v.ID, a.ID); err != nil {
		t.Fatalf("new summary cannot be visualized: %v", err)
	}
	visualJobs, err := s.QueuedInfographics(ctx)
	if err != nil || len(visualJobs) != 1 {
		t.Fatalf("queued visual: %+v %v", visualJobs, err)
	}
	if _, err := s.Enqueue(ctx, v.ID, a.ID, summary.Detailed, ""); err != nil {
		t.Fatal(err)
	}
	_, summaryLease, err = s.NextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, a.ID, summaryLease, "再更新", []string{"最新版"}, "test", nil, "", []string{"最新の結論"}); err != nil {
		t.Fatal(err)
	}
	if stale, _, err := s.ClaimInfographic(ctx, a.ID, visualJobs[0].QueuedAt); err != nil || stale != nil {
		t.Fatalf("obsolete visual job survived summary update: %+v %v", stale, err)
	}
}
