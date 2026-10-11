package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"my-bookmark/internal/content"
	"my-bookmark/internal/infographic"
	"my-bookmark/internal/store"
)

type imageWorkerDB struct {
	InfographicQueue
	finished bool
	failure  string
	picture  *infographic.Image
}

func (d *imageWorkerDB) Get(context.Context, string) (store.Article, error) {
	return store.Article{Title: "蓄電池", TLDR: []string{"電気を蓄える"}, Points: []string{"充電して放電する"}}, nil
}
func (d *imageWorkerDB) FinishInfographicImage(_ context.Context, id, lease string, picture *infographic.Image, failure string) error {
	d.finished, d.failure, d.picture = true, failure, picture
	return nil
}

type imageGeneratorFunc func(context.Context, string, []string, []string) (infographic.Image, error)

func (f imageGeneratorFunc) GenerateInfographic(ctx context.Context, title string, tldr, points []string) (infographic.Image, error) {
	return f(ctx, title, tldr, points)
}

func TestInfographicWorkerPublishesImageAndHandlesFailure(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}
	picture, err := infographic.PrepareImage(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                    string
		result                  infographic.Image
		err                     error
		retry                   bool
		wantFinish, wantFailure bool
	}{
		{"image", picture, nil, true, true, false},
		{"invalid image", infographic.Image{}, nil, true, true, true},
		{"retry transient", infographic.Image{}, &content.APIError{Status: 429}, true, false, false},
		{"retry exhausted", infographic.Image{}, &content.APIError{Status: 429}, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := &imageWorkerDB{}
			generator := imageGeneratorFunc(func(_ context.Context, title string, tldr, points []string) (infographic.Image, error) {
				if title != "蓄電池" || len(tldr) != 1 || len(points) != 1 {
					t.Fatal("missing article facts")
				}
				return tc.result, tc.err
			})
			err := NewInfographicWorker(db, generator).process(context.Background(), "article", "lease", tc.retry)
			if db.finished != tc.wantFinish || (db.failure != "") != tc.wantFailure {
				t.Fatalf("finish=%v failure=%q err=%v", db.finished, db.failure, err)
			}
			if !tc.wantFinish && err == nil {
				t.Fatal("transient failure was swallowed")
			}
			if tc.wantFinish && !tc.wantFailure && (db.picture == nil || !bytes.Equal(db.picture.Data, picture.Data)) {
				t.Fatal("image was not published")
			}
			if tc.wantFailure && db.picture != nil {
				t.Fatal("failed generation published an image")
			}
		})
	}
}
