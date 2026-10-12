// Package cost estimates Standard Vertex AI API charges, excluding infrastructure.
package cost

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Rates verified against Google Cloud pricing on 2026-10-11.
// https://cloud.google.com/gemini-enterprise-agent-platform/generative-ai/pricing
const USDJPY = 150.0
const Explanation = "直近の生成に使ったAPI料金の概算（自動再試行・画像の構図設計・確認・修正を含む）。1ドル150円で換算。税・サーバー料金・割引は含みません。"

type Estimate struct {
	USD      float64 `firestore:"usd"`
	Requests int64   `firestore:"requests"`
	Unknown  int64   `firestore:"unknown"`
}

func (e Estimate) Label() string {
	if e.Requests == 0 {
		return ""
	}
	if e.Unknown > 0 {
		return "推定コスト：算出できません"
	}
	if e.USD > 0 && e.USD*USDJPY < 0.01 {
		return "推定コスト：¥0.01未満"
	}
	return fmt.Sprintf("推定コスト：約¥%.2f", e.USD*USDJPY)
}

type Usage struct {
	Prompt     int64 `json:"promptTokenCount"`
	Candidates int64 `json:"candidatesTokenCount"`
	Thoughts   int64 `json:"thoughtsTokenCount"`
	Cached     int64 `json:"cachedContentTokenCount"`
	Details    []struct {
		Modality string `json:"modality"`
		Count    int64  `json:"tokenCount"`
	} `json:"candidatesTokensDetails"`
}

func Calculate(model, location string, u *Usage, image bool, now time.Time) Estimate {
	e := Estimate{Requests: 1}
	unknown := func() Estimate { e.Unknown = 1; return e }
	if u == nil || u.Prompt < 0 || u.Candidates < 0 || u.Thoughts < 0 || u.Cached < 0 || u.Cached > u.Prompt {
		return unknown()
	}
	var input, output, imageRate float64
	switch model {
	case "gemini-3.8-flash":
		input, output = 0.75, 3.75
		if !now.Before(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
			input, output = 1.5, 7.5
		}
	case "gemini-3-pro-image":
		if location != "global" {
			return unknown()
		}
		input, output, imageRate = 2, 12, 120
	default:
		return unknown()
	}
	var imageTokens int64
	for _, d := range u.Details {
		if strings.EqualFold(d.Modality, "IMAGE") {
			imageTokens += d.Count
		}
	}
	// Current image requests always ask for 2K: 1120 output tokens per image.
	if image && imageTokens == 0 && u.Candidates >= 1120 {
		imageTokens = 1120
	}
	if imageTokens < 0 || imageTokens > u.Candidates || (image && imageTokens == 0) || (!image && imageTokens > 0) {
		return unknown()
	}
	e.USD = (float64(u.Prompt-u.Cached)*input + float64(u.Cached)*input/10 + float64(u.Candidates-imageTokens+u.Thoughts)*output + float64(imageTokens)*imageRate) / 1e6
	if location != "global" && model == "gemini-3.8-flash" {
		e.USD *= 1.1
	}
	return e
}

type recorderKey struct{}

func WithRecorder(ctx context.Context, record func(Estimate)) context.Context {
	return context.WithValue(ctx, recorderKey{}, record)
}

func Record(ctx context.Context, model, location string, usage *Usage, image bool) {
	if record, ok := ctx.Value(recorderKey{}).(func(Estimate)); ok {
		record(Calculate(model, location, usage, image, time.Now()))
	}
}
