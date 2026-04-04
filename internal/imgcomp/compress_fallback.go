//go:build !vips

package imgcomp

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png"
)

// CompressImage compresses image data to JPEG using pure Go stdlib.
// Downscales large images and adjusts quality to fit under Threshold bytes.
// Returns the compressed bytes and the output content type.
func CompressImage(imgData []byte) ([]byte, string, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, "", err
	}

	img = downscale(img, maxDim)

	// Binary search for highest quality that fits under Threshold
	lo, hi := 10, 80
	var best []byte
	for lo <= hi {
		mid := (lo + hi) / 2
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: mid}); err != nil {
			return nil, "", err
		}
		if buf.Len() <= Threshold {
			best = buf.Bytes()
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best != nil {
		return best, "image/jpeg", nil
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 10}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

func downscale(img image.Image, maxDim int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return img
	}
	scale := float64(maxDim) / float64(w)
	if h > w {
		scale = float64(maxDim) / float64(h)
	}
	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := b.Min.Y + y*h/newH
		for x := 0; x < newW; x++ {
			srcX := b.Min.X + x*w/newW
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
	return dst
}
