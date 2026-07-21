// Package thumbnail generates small, fast-loading JPEG previews of photos so
// the file grid doesn't have to stream full-resolution originals (some of
// them tens of MB) just to paint a 160px card — that was making folders full
// of photos crawl to load. Deliberately stdlib-only (image/jpeg, image/png,
// image/gif): no third-party resize library is vendored in this project, and
// this environment can't fetch a new dependency on demand.
package thumbnail

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
)

// MaxDimension bounds both width and height of a generated thumbnail —
// large enough to look sharp in the file grid/list views, small enough to
// stay well under 100KB for a typical photo.
const MaxDimension = 480

// JPEGQuality trades a little detail for a much smaller file — thumbnails
// are decorative, not archival, so this favors load speed.
const JPEGQuality = 82

// ErrUnsupportedFormat means image.Decode couldn't recognize the source
// bytes as jpeg/png/gif (e.g. webp, tiff, bmp, svg — formats the Go
// standard library doesn't decode). Callers should fall back to the
// original file or a generic icon rather than erroring out the request.
var ErrUnsupportedFormat = errors.New("thumbnail: unsupported image format")

// Generate decodes an image and returns a downscaled JPEG re-encoding of it,
// fit within MaxDimension×MaxDimension while preserving aspect ratio.
// Images already smaller than that are not upscaled, just re-encoded.
func Generate(r io.Reader) ([]byte, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, ErrUnsupportedFormat
	}

	small := downscale(img, MaxDimension)

	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, small, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// downscale box-samples src down to fit within maxDim on its longer side.
// Each destination pixel is the average of the source pixels it covers, then
// composited over a white background (thumbnails are always plain JPEG, so
// any source transparency needs to be flattened onto something).
func downscale(src image.Image, maxDim int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}

	scale := 1.0
	if w > maxDim || h > maxDim {
		sw := float64(maxDim) / float64(w)
		sh := float64(maxDim) / float64(h)
		if sw < sh {
			scale = sw
		} else {
			scale = sh
		}
	}
	if scale >= 1.0 {
		// Already small enough — still route through the box sampler at 1:1
		// so the flatten-onto-white step runs uniformly for every image.
		scale = 1.0
	}

	nw := max(1, int(float64(w)*scale))
	nh := max(1, int(float64(h)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0 := b.Min.Y + int(float64(y)*float64(h)/float64(nh))
		sy1 := b.Min.Y + int(float64(y+1)*float64(h)/float64(nh))
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		if sy1 > b.Max.Y {
			sy1 = b.Max.Y
		}
		for x := 0; x < nw; x++ {
			sx0 := b.Min.X + int(float64(x)*float64(w)/float64(nw))
			sx1 := b.Min.X + int(float64(x+1)*float64(w)/float64(nw))
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if sx1 > b.Max.X {
				sx1 = b.Max.X
			}

			var rSum, gSum, bSum, aSum, count uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					r, g, bl, a := src.At(sx, sy).RGBA()
					rSum += uint64(r)
					gSum += uint64(g)
					bSum += uint64(bl)
					aSum += uint64(a)
					count++
				}
			}
			if count == 0 {
				count = 1
			}
			// src.At(...).RGBA() returns alpha-premultiplied 16-bit
			// components with component <= alpha always holding, so
			// component + (0xffff-alpha) — flattening onto opaque white —
			// can never exceed 0xffff and needs no clamping.
			ar := aSum / count
			white := uint64(0xffff) - ar
			r8 := uint8((rSum/count + white) >> 8)
			g8 := uint8((gSum/count + white) >> 8)
			b8 := uint8((bSum/count + white) >> 8)
			dst.Set(x, y, rgba{r8, g8, b8})
		}
	}
	return dst
}

// rgba is a minimal opaque color.Color so downscale doesn't need to import
// image/color just for a four-field struct literal.
type rgba struct{ R, G, B uint8 }

func (c rgba) RGBA() (r, g, b, a uint32) {
	r = uint32(c.R) * 0x101
	g = uint32(c.G) * 0x101
	b = uint32(c.B) * 0x101
	a = 0xffff
	return
}
