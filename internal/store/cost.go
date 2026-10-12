package store

import (
	"context"

	"cloud.google.com/go/firestore"
	"my-bookmark/internal/cost"
)

// RecordGenerationCost only accepts usage from the current job owner.
func (s *Store) RecordGenerationCost(ctx context.Context, id, lease string, visual bool, usage cost.Estimate) error {
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ar := s.article(id)
		ad, err := tx.Get(ar)
		if err != nil {
			return err
		}
		var a Article
		if err := ad.DataTo(&a); err != nil {
			return err
		}
		collection, field := "summary_jobs", "summary_cost"
		if visual {
			collection, field = "infographic_jobs", "infographic_cost"
		}
		jd, err := tx.Get(s.client.Collection(collection).Doc(id))
		if err != nil {
			return err
		}
		var job struct {
			Lease          string `firestore:"lease"`
			Status         string `firestore:"status"`
			SummaryVersion int64  `firestore:"summary_version"`
		}
		if err := jd.DataTo(&job); err != nil {
			return err
		}
		if lease == "" || job.Lease != lease || job.Status != "running" || !a.Active {
			return ErrBusy
		}
		if visual && job.SummaryVersion != a.SummaryVersion || !visual && a.Lease != lease {
			return ErrBusy
		}
		total := a.SummaryCost
		if visual {
			total = a.InfographicCost
		}
		total.USD += usage.USD
		total.Requests += usage.Requests
		total.Unknown += usage.Unknown
		return tx.Update(ar, []firestore.Update{{Path: field, Value: total}})
	})
}
