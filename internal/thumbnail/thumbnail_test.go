package thumbnail

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// makePNG builds a w x h solid-color test image encoded as PNG, standing in
// for a real uploaded photo without needing a fixture file on disk.
func makePNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func decodeJPEGDimensions(t *testing.T, data []byte) (w, h int) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Generate did not produce a valid JPEG: %v", err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

func TestGenerateDownscalesLargeLandscapeImage(t *testing.T) {
	src := makePNG(t, 2000, 1000, color.RGBA{200, 50, 50, 255})
	out, err := Generate(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := decodeJPEGDimensions(t, out)
	if w != MaxDimension {
		t.Errorf("width = %d, want %d (longer side clamped to MaxDimension)", w, MaxDimension)
	}
	wantH := MaxDimension / 2 // aspect ratio 2:1 preserved
	if h < wantH-1 || h > wantH+1 {
		t.Errorf("height = %d, want ~%d (aspect ratio preserved)", h, wantH)
	}
}

func TestGenerateDownscalesLargePortraitImage(t *testing.T) {
	src := makePNG(t, 900, 1800, color.RGBA{50, 200, 50, 255})
	out, err := Generate(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := decodeJPEGDimensions(t, out)
	if h != MaxDimension {
		t.Errorf("height = %d, want %d (longer side clamped to MaxDimension)", h, MaxDimension)
	}
	wantW := MaxDimension / 2
	if w < wantW-1 || w > wantW+1 {
		t.Errorf("width = %d, want ~%d (aspect ratio preserved)", w, wantW)
	}
}

func TestGenerateDoesNotUpscaleSmallImage(t *testing.T) {
	src := makePNG(t, 40, 30, color.RGBA{50, 50, 200, 255})
	out, err := Generate(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := decodeJPEGDimensions(t, out)
	if w != 40 || h != 30 {
		t.Errorf("size = %dx%d, want unchanged 40x30 (already below MaxDimension)", w, h)
	}
}

func TestGenerateSquareImageStaysSquare(t *testing.T) {
	src := makePNG(t, 1000, 1000, color.RGBA{10, 10, 10, 255})
	out, err := Generate(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := decodeJPEGDimensions(t, out)
	if w != MaxDimension || h != MaxDimension {
		t.Errorf("size = %dx%d, want %dx%d", w, h, MaxDimension, MaxDimension)
	}
}

func TestGenerateFlattensTransparencyOntoWhite(t *testing.T) {
	// A fully transparent pixel must come out solid white (thumbnails are
	// plain JPEG, which has no alpha channel to preserve it in).
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{0, 0, 0, 0}) // fully transparent
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := Generate(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	r, g, b, _ := decoded.At(5, 5).RGBA()
	if r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
		t.Errorf("transparent pixel did not flatten to white: got RGB(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestGenerateUnsupportedFormat(t *testing.T) {
	_, err := Generate(strings.NewReader("this is not an image, just plain text"))
	if err != ErrUnsupportedFormat {
		t.Errorf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestGenerateEmptyInput(t *testing.T) {
	_, err := Generate(bytes.NewReader(nil))
	if err != ErrUnsupportedFormat {
		t.Errorf("err = %v, want ErrUnsupportedFormat", err)
	}
}
