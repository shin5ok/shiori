package cost

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestUsagePricing(t *testing.T) {
	date := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, model, location, payload string
		image                          bool
		usd                            float64
		unknown                        int64
	}{
		{"cached input and reasoning", "gemini-3.8-flash", "global", `{"promptTokenCount":1000,"cachedContentTokenCount":200,"candidatesTokenCount":100,"thoughtsTokenCount":50}`, false, .0011775, 0},
		{"regional surcharge", "gemini-3.8-flash", "asia-northeast1", `{"promptTokenCount":1000,"candidatesTokenCount":100}`, false, .0012375, 0},
		{"image and text", "gemini-3-pro-image", "global", `{"promptTokenCount":1000,"candidatesTokenCount":1220,"thoughtsTokenCount":50,"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":1120},{"modality":"TEXT","tokenCount":100}]}`, true, .1382, 0},
		{"2K fallback", "gemini-3-pro-image", "global", `{"promptTokenCount":1000,"candidatesTokenCount":1120}`, true, .1364, 0},
		{"unknown model", "future-model", "global", `{}`, false, 0, 1},
		{"missing metadata", "gemini-3.8-flash", "global", `null`, false, 0, 1},
		{"invalid cached count", "gemini-3.8-flash", "global", `{"promptTokenCount":1,"cachedContentTokenCount":2}`, false, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var u *Usage
			if err := json.Unmarshal([]byte(tc.payload), &u); err != nil {
				t.Fatal(err)
			}
			got := Calculate(tc.model, tc.location, u, tc.image, date)
			if math.Abs(got.USD-tc.usd) > 1e-10 || got.Unknown != tc.unknown || got.Requests != 1 {
				t.Fatalf("estimate: %+v", got)
			}
		})
	}
	got := Calculate("gemini-3.8-flash", "global", &Usage{Prompt: 1000000}, false, date.AddDate(1, 0, 0))
	if got.USD != 1.5 {
		t.Fatalf("2027 rate: %+v", got)
	}
}

func TestLabels(t *testing.T) {
	if (Estimate{}).Label() != "" {
		t.Fatal("historical records should not claim zero cost")
	}
	if got := (Estimate{USD: .1364, Requests: 1}).Label(); got != "推定コスト：約¥20.46" {
		t.Fatal(got)
	}
	if got := (Estimate{USD: .000001, Requests: 1}).Label(); got != "推定コスト：¥0.01未満" {
		t.Fatal(got)
	}
	if got := (Estimate{USD: .1, Requests: 2, Unknown: 1}).Label(); got != "推定コスト：算出できません" {
		t.Fatal(got)
	}
}
