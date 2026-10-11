package infographic

import (
	"bytes"
	"image"
	"image/png"
	"math/rand"
	"testing"
)

func TestPrepareImagePreservesDimensionsWithinStorageBudget(t *testing.T) {
	for _, noisy := range []bool{false, true} {
		source := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
		if noisy {
			_, _ = rand.New(rand.NewSource(7)).Read(source.Pix)
		}
		var raw bytes.Buffer
		if err := png.Encode(&raw, source); err != nil {
			t.Fatal(err)
		}
		picture, err := PrepareImage(raw.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if err := picture.Validate(); err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(picture.Data))
		if err != nil || cfg.Width != 1024 || cfg.Height != 1024 || len(picture.Data) > MaxImageBytes {
			t.Fatalf("invalid converted image: %+v %v", cfg, err)
		}
		if noisy && picture.MIME != "image/jpeg" {
			t.Fatal("large PNG was not compressed")
		}
	}
}

func TestPrepareImageRejectsInvalidImages(t *testing.T) {
	for _, data := range [][]byte{nil, []byte(`<svg onload="alert(1)"/>`), bytes.Repeat([]byte{'x'}, 20*1024*1024+1)} {
		if _, err := PrepareImage(data); err == nil {
			t.Fatal("accepted invalid image")
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	if _, err := PrepareImage(buf.Bytes()); err == nil {
		t.Fatal("accepted tiny image")
	}
}
