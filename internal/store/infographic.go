package store

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"my-bookmark/internal/cost"
	"my-bookmark/internal/infographic"
)

func (s *Store) infographicJob(id string) *firestore.DocumentRef {
	return s.client.Collection("infographic_jobs").Doc(id)
}

func (s *Store) infographicImage(id string) *firestore.DocumentRef {
	return s.client.Collection("infographic_images").Doc(id)
}

type storedInfographicImage struct {
	Image infographic.Image `firestore:"image"`
	Key   string            `firestore:"key"`
}

func (s *Store) GetInfographicImage(ctx context.Context, id, key string) (infographic.Image, error) {
	doc, err := s.infographicImage(id).Get(ctx)
	if err != nil {
		return infographic.Image{}, normalizeError(err)
	}
	var saved storedInfographicImage
	if err := doc.DataTo(&saved); err != nil {
		return infographic.Image{}, err
	}
	if key == "" || saved.Key != key {
		return infographic.Image{}, ErrNotFound
	}
	if err := saved.Image.Validate(); err != nil {
		return infographic.Image{}, err
	}
	return saved.Image, nil
}

func (s *Store) EnqueueInfographic(ctx context.Context, uid, id string) error {
	now := time.Now().UTC()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if _, err := tx.Get(s.bookmark(uid, id)); err != nil {
			return normalizeError(err)
		}
		ar, jr := s.article(id), s.infographicJob(id)
		ad, err := tx.Get(ar)
		if err != nil {
			return normalizeError(err)
		}
		jd, je := tx.Get(jr)
		if je != nil && !missing(je) {
			return je
		}
		var a Article
		if err := ad.DataTo(&a); err != nil {
			return err
		}
		if !a.Active || a.Status != "ready" || len(a.Points) == 0 {
			return ErrNotReady
		}
		if je == nil {
			var j InfographicJob
			if err := jd.DataTo(&j); err != nil {
				return err
			}
			if j.Status == "queued" || j.Status == "dispatched" || j.Status == "running" && j.LeaseUntil.After(now) {
				return ErrBusy
			}
		}
		q, qr, err := readQuota(ctx, tx, s.client, uid, now)
		if err != nil {
			return err
		}
		if !consumeQuota(&q, now) {
			return ErrRateLimited
		}
		j := InfographicJob{ArticleID: id, UserID: uid, SummaryVersion: a.SummaryVersion, Status: "queued", QueuedAt: now}
		a.InfographicCost = cost.Estimate{}
		a.InfographicStatus, a.InfographicError = "queued", ""
		a.InfographicLeaseUntil = time.Time{}
		if err := tx.Set(ar, a); err != nil {
			return err
		}
		if err := tx.Set(jr, j); err != nil {
			return err
		}
		return tx.Set(qr, q)
	})
}

func (s *Store) QueuedInfographics(ctx context.Context) ([]InfographicJob, error) {
	docs, err := s.client.Collection("infographic_jobs").Where("status", "==", "queued").OrderBy("queued_at", firestore.Asc).Limit(10).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	jobs := make([]InfographicJob, 0, len(docs))
	for _, d := range docs {
		var j InfographicJob
		if err := d.DataTo(&j); err != nil {
			return nil, err
		}
		j.ArticleID = d.Ref.ID
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *Store) MarkInfographicDispatched(ctx context.Context, id string, queuedAt time.Time) error {
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.infographicJob(id)
		d, err := tx.Get(ref)
		if missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var j InfographicJob
		if err := d.DataTo(&j); err != nil {
			return err
		}
		if j.Status != "queued" || !j.QueuedAt.Equal(queuedAt) {
			return nil
		}
		return tx.Update(ref, []firestore.Update{{Path: "status", Value: "dispatched"}})
	})
}

func (s *Store) ClaimInfographic(ctx context.Context, id string, queuedAt time.Time) (*InfographicJob, string, error) {
	lease := RandomToken()
	var claimed *InfographicJob
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		claimed = nil
		jr, ar := s.infographicJob(id), s.article(id)
		jd, err := tx.Get(jr)
		if missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var j InfographicJob
		if err := jd.DataTo(&j); err != nil {
			return err
		}
		if !j.QueuedAt.Equal(queuedAt) || j.Status != "queued" && j.Status != "dispatched" && j.Status != "running" {
			return nil
		}
		now := time.Now().UTC()
		if j.Status == "running" && j.LeaseUntil.After(now) {
			return ErrBusy
		}
		ad, err := tx.Get(ar)
		if err != nil {
			return err
		}
		var a Article
		if err := ad.DataTo(&a); err != nil {
			return err
		}
		if !a.Active || a.Status != "ready" || a.SummaryVersion != j.SummaryVersion {
			j.Status = "cancelled"
			if a.InfographicStatus != "ready" {
				a.InfographicStatus = ""
			}
		} else {
			j.ArticleID = id
			j.Status = "running"
			j.Lease, j.LeaseUntil = lease, now.Add(JobLeaseDuration)
			j.Attempts++
			a.InfographicStatus, a.InfographicLeaseUntil = "processing", j.LeaseUntil
			claimed = &j
		}
		if err := tx.Set(jr, j); err != nil {
			return err
		}
		return tx.Set(ar, a)
	})
	return claimed, lease, err
}

func (s *Store) NextInfographic(ctx context.Context) (*InfographicJob, string, error) {
	jobs, err := s.QueuedInfographics(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, j := range jobs {
		job, lease, err := s.ClaimInfographic(ctx, j.ArticleID, j.QueuedAt)
		if errors.Is(err, ErrBusy) {
			continue
		}
		if err != nil || job != nil {
			return job, lease, err
		}
	}
	return nil, "", nil
}

func (s *Store) FinishInfographic(ctx context.Context, id, lease string, plan *infographic.Plan, failure string) error {
	return s.finishInfographic(ctx, id, lease, plan, nil, failure)
}

func (s *Store) FinishInfographicImage(ctx context.Context, id, lease string, picture *infographic.Image, failure string) error {
	if picture != nil {
		if err := picture.Validate(); err != nil {
			return err
		}
	}
	return s.finishInfographic(ctx, id, lease, nil, picture, failure)
}

func (s *Store) finishInfographic(ctx context.Context, id, lease string, plan *infographic.Plan, picture *infographic.Image, failure string) error {
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		jr, ar := s.infographicJob(id), s.article(id)
		jd, err := tx.Get(jr)
		if err != nil {
			return err
		}
		ad, err := tx.Get(ar)
		if err != nil {
			return err
		}
		var j InfographicJob
		var a Article
		if err := jd.DataTo(&j); err != nil {
			return err
		}
		if err := ad.DataTo(&a); err != nil {
			return err
		}
		if j.Status != "running" || j.Lease != lease || a.SummaryVersion != j.SummaryVersion || !a.Active {
			return ErrBusy
		}
		j.Lease, j.LeaseUntil = "", time.Time{}
		a.InfographicLeaseUntil = time.Time{}
		if failure == "" && picture != nil {
			key := Hash(string(picture.Data))
			if err := tx.Set(s.infographicImage(id), storedInfographicImage{Image: *picture, Key: key}); err != nil {
				return err
			}
			a.Infographic, a.InfographicImageKey = nil, key
			a.InfographicStatus, a.InfographicError, j.Status = "ready", "", "done"
		} else if failure == "" && plan != nil {
			if err := plan.Validate(); err != nil {
				return err
			}
			a.Infographic, a.InfographicStatus, a.InfographicError = plan, "ready", ""
			a.InfographicImageKey = ""
			if err := tx.Delete(s.infographicImage(id)); err != nil {
				return err
			}
			j.Status = "done"
		} else {
			a.InfographicStatus, a.InfographicError = "failed", failure
			j.Status = "failed"
		}
		if err := tx.Set(jr, j); err != nil {
			return err
		}
		return tx.Set(ar, a)
	})
}

func (s *Store) RetryInfographic(ctx context.Context, id, lease string) error {
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		jr, ar := s.infographicJob(id), s.article(id)
		jd, err := tx.Get(jr)
		if err != nil {
			return err
		}
		ad, err := tx.Get(ar)
		if err != nil {
			return err
		}
		var j InfographicJob
		var a Article
		if err := jd.DataTo(&j); err != nil {
			return err
		}
		if err := ad.DataTo(&a); err != nil {
			return err
		}
		if j.Status != "running" || j.Lease != lease {
			return ErrBusy
		}
		j.Status, j.Lease, j.LeaseUntil = "dispatched", "", time.Time{}
		a.InfographicStatus, a.InfographicLeaseUntil = "queued", time.Time{}
		if err := tx.Set(jr, j); err != nil {
			return err
		}
		return tx.Set(ar, a)
	})
}
