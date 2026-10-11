package infographic

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
)

// Images live in a separate document below Firestore's 1 MiB limit.
const MaxImageBytes = 850 * 1024

type Image struct {
	Data []byte `firestore:"data"`
	MIME string `firestore:"mime"`
}

func (i Image) Validate() error {
	if len(i.Data) == 0 || len(i.Data) > MaxImageBytes {
		return errors.New("invalid infographic image size")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(i.Data))
	if err != nil || (format != "png" && format != "jpeg") || i.MIME != "image/"+format || !validImageDimensions(cfg) {
		return errors.New("invalid infographic image")
	}
	return nil
}

// PrepareImage strips metadata and preserves resolution within the size budget.
func PrepareImage(data []byte) (Image, error) {
	if len(data) == 0 || len(data) > 20*1024*1024 {
		return Image{}, errors.New("invalid generated image size")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || !validImageDimensions(cfg) {
		return Image{}, errors.New("invalid generated image dimensions or format")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, source); err != nil {
		return Image{}, err
	}
	if buf.Len() <= MaxImageBytes {
		return Image{Data: buf.Bytes(), MIME: "image/png"}, nil
	}
	flat := image.NewRGBA(source.Bounds())
	draw.Draw(flat, flat.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), source, source.Bounds().Min, draw.Over)
	for _, quality := range []int{92, 85, 78, 70, 60} {
		buf.Reset()
		if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: quality}); err != nil {
			return Image{}, err
		}
		if buf.Len() <= MaxImageBytes {
			return Image{Data: buf.Bytes(), MIME: "image/jpeg"}, nil
		}
	}
	return Image{}, errors.New("generated image exceeds storage size limit")
}

func validImageDimensions(cfg image.Config) bool {
	return cfg.Width >= 256 && cfg.Height >= 256 && cfg.Width <= 4096 && cfg.Height <= 4096 && int64(cfg.Width)*int64(cfg.Height) <= 12_000_000
}
