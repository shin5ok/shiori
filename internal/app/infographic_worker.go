package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"my-bookmark/internal/content"
	"my-bookmark/internal/infographic"
	"my-bookmark/internal/store"
)

type InfographicQueue interface {
	NextInfographic(context.Context) (*store.InfographicJob, string, error)
	Get(context.Context, string) (store.Article, error)
	FinishInfographicImage(context.Context, string, string, *infographic.Image, string) error
}

type InfographicGenerator interface {
	GenerateInfographic(context.Context, string, []string, []string) (infographic.Image, error)
}

type InfographicWorker struct {
	db        InfographicQueue
	generator InfographicGenerator
}

func NewInfographicWorker(db InfographicQueue, generator InfographicGenerator) *InfographicWorker {
	return &InfographicWorker{db: db, generator: generator}
}

func (w *InfographicWorker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		job, lease, err := w.db.NextInfographic(ctx)
		if err != nil {
			slog.Error("infographic queue scan failed", "error", err)
			if !wait(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if job == nil {
			if !wait(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if err := w.process(ctx, job.ArticleID, lease, false); err != nil {
			slog.Error("infographic processing failed", "article", job.ArticleID, "error", err)
		}
	}
}

func (w *InfographicWorker) process(parent context.Context, id, lease string, retryTransient bool) error {
	ctx, cancel := context.WithTimeout(parent, 9*time.Minute)
	defer cancel()
	article, err := w.db.Get(ctx, id)
	var picture infographic.Image
	if err == nil && len(article.Points) > 0 && w.generator != nil {
		picture, err = w.generator.GenerateInfographic(ctx, article.Title, article.TLDR, article.Points)
		if err == nil {
			err = picture.Validate()
		}
	} else if err == nil {
		err = store.ErrNotReady
	}
	if retryTransient && (ctx.Err() != nil || content.Retryable(err) || errors.Is(err, context.DeadlineExceeded)) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	failure := ""
	var result *infographic.Image
	if err != nil {
		slog.Error("infographic generation failed", "article", id, "error", err)
		failure = "図解を作成できませんでした。再試行できます。"
	} else {
		result = &picture
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer finishCancel()
	return w.db.FinishInfographicImage(finishCtx, id, lease, result, failure)
}
