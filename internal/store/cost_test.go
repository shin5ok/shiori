package store

import (
	"errors"
	"testing"

	"my-bookmark/internal/cost"
	"my-bookmark/internal/summary"
)

func TestGenerationCostOwnershipAndRetry(t *testing.T) {
	s, ctx := ratingTestStore(t)
	u := User{ID: RandomToken(), Name: "reader"}
	a, err := s.Save(ctx, u, "https://example.com/"+RandomToken(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	job, lease, err := s.NextQueued(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim: %v", err)
	}
	usage := cost.Estimate{USD: .01, Requests: 1}
	if err := s.RecordGenerationCost(ctx, a.ID, "stale", false, usage); !errors.Is(err, ErrBusy) {
		t.Fatalf("stale lease: %v", err)
	}
	if err := s.RecordGenerationCost(ctx, a.ID, lease, false, usage); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryJob(ctx, a.ID, lease); err != nil {
		t.Fatal(err)
	}
	_, next, err := s.ClaimJob(ctx, a.ID, job.QueuedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordGenerationCost(ctx, a.ID, next, false, usage); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, a.ID, next, "記事", []string{"要点"}, "test", nil, "", nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil || got.SummaryCost.USD != .02 || got.SummaryCost.Requests != 2 {
		t.Fatalf("retry total: %+v %v", got.SummaryCost, err)
	}
	if err := s.EnqueueInfographic(ctx, u.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	_, visualLease, err := s.NextInfographic(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordGenerationCost(ctx, a.ID, visualLease, true, usage); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishInfographicImage(ctx, a.ID, visualLease, nil, "failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, u.ID, a.ID, summary.Detailed, ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(ctx, a.ID)
	if err != nil || got.SummaryCost.Requests != 0 || got.InfographicCost.Requests != 1 {
		t.Fatalf("new generation: %+v %v", got, err)
	}
}
