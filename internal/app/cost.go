package app

import (
	"context"
	"log/slog"
	"time"

	"my-bookmark/internal/cost"
)

func generationCostContext(ctx context.Context, db any, id, lease string, visual bool) context.Context {
	saver, ok := db.(interface {
		RecordGenerationCost(context.Context, string, string, bool, cost.Estimate) error
	})
	if !ok {
		return ctx
	}
	return cost.WithRecorder(ctx, func(e cost.Estimate) {
		// Record each completed API call while this attempt still owns its lease.
		// This also preserves charges when a later call fails or the task retries.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := saver.RecordGenerationCost(saveCtx, id, lease, visual, e); err != nil {
			slog.Error("generation cost persistence failed", "article", id, "error", err)
		}
	})
}
