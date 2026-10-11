package content

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func infographicPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGeminiInfographicArtDirectsThenGeneratesImage(t *testing.T) {
	g := &Gemini{Project: "test", Location: "global", Model: "text-model", ImageModel: "image-model", Endpoint: "https://vertex.example", TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}
	calls := 0
	data := infographicPNG(t)
	g.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(payload)
		if r.Header.Get("Authorization") != "Bearer token" || !strings.Contains(string(body), "蓄電池") {
			t.Fatalf("missing authentication or source facts: %s", body)
		}
		w := httptest.NewRecorder()
		if calls == 1 {
			if !strings.Contains(r.URL.Path, "text-model:generateContent") || !strings.Contains(string(body), "composition") || strings.Contains(string(body), `"kind"`) {
				t.Fatalf("expected free art direction: %s", body)
			}
			writeGeminiImagesResponse(w, `{"composition":"大きな蓄電池の断面に電気の経路を描く","art_direction":"精密な説明イラスト","labels":["充電","蓄電池","放電"],"aspect_ratio":"3:4"}`)
		} else if calls == 2 {
			if !strings.Contains(r.URL.Path, "/locations/global/publishers/google/models/image-model:generateContent") || !strings.Contains(string(body), `"responseModalities":["TEXT","IMAGE"]`) || !strings.Contains(string(body), `"aspectRatio":"3:4"`) || !strings.Contains(string(body), "大きな蓄電池") {
				t.Fatalf("invalid image request: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{
				map[string]any{"thought": true, "inlineData": map[string]any{"mimeType": "image/png", "data": []byte("ignore thought image")}},
				map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": data}},
			}}}}})
		} else {
			if !strings.Contains(r.URL.Path, "text-model:generateContent") || !strings.Contains(string(body), "inlineData") {
				t.Fatal("image was not reviewed")
			}
			writeGeminiImagesResponse(w, `{"accurate":true,"issues":[]}`)
		}
		return w.Result(), nil
	})}
	picture, err := g.GenerateInfographic(context.Background(), "蓄電池のしくみ", []string{"電気を蓄える"}, []string{"充電と放電の流れ"})
	if err != nil || picture.Validate() != nil || calls != 3 {
		t.Fatalf("calls=%d mime=%s err=%v", calls, picture.MIME, err)
	}
}

func TestGeminiInfographicRepairsSourceErrorsBeforePublishing(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		g := &Gemini{Project: "test", Location: "global", Model: "text-model", ImageModel: "image-model", TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}
		calls := 0
		data := infographicPNG(t)
		g.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			switch calls {
			case 1:
				writeGeminiImagesResponse(w, `{"composition":"昼夜の家","art_direction":"説明イラスト","labels":["発電","充電","放電"],"aspect_ratio":"4:3"}`)
			case 2, 4:
				if calls == 4 {
					body, _ := json.Marshal(request)
					if !strings.Contains(string(body), "100%を削除") || !strings.Contains(string(body), "inlineData") {
						t.Fatal("repair request missing draft or correction")
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": data}}}}}}})
			case 3, 5:
				if calls == 5 && accepted {
					writeGeminiImagesResponse(w, `{"accurate":true,"issues":[]}`)
				} else {
					writeGeminiImagesResponse(w, `{"accurate":false,"issues":["出典にない100%を削除"]}`)
				}
			default:
				t.Fatal("unbounded repair loop")
			}
			return w.Result(), nil
		})}
		_, err := g.GenerateInfographic(context.Background(), "太陽光", []string{"昼に発電"}, []string{"夜に使う"})
		if calls != 5 || (err == nil) != accepted {
			t.Fatalf("accepted=%v calls=%d err=%v", accepted, calls, err)
		}
	}
}

func TestGeminiInfographicRejectsMissingAndUnfinishedImages(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"text only", `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"No image"}]}}]}`, 200},
		{"unfinished", `{"candidates":[{"finishReason":"MAX_TOKENS"}]}`, 200},
		{"unsafe format", `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"inlineData":{"mimeType":"image/svg+xml","data":"PHN2Zy8+"}}]}}]}`, 200},
		{"busy", "", 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &Gemini{Project: "test", ImageModel: "image-model", TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}
			g.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				w.WriteHeader(tc.status)
				w.WriteString(tc.body)
				return w.Result(), nil
			})}
			_, err := g.generateInfographicImage(context.Background(), "draw", "4:3")
			if err == nil {
				t.Fatal("accepted invalid image response")
			}
			if tc.status == 429 && !Retryable(err) {
				t.Fatal("lost transient API error")
			}
		})
	}
}
