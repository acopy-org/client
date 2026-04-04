//go:build cgo

package imgcomp

/*
#include "stb_image.h"
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"fmt"
	"image"
	"unsafe"

	"github.com/chai2010/webp"
	"golang.org/x/image/draw"
)

// decodeImage uses stb_image (C) for fast PNG/JPEG decoding.
func decodeImage(data []byte) (*image.NRGBA, error) {
	var w, h, channels C.int
	cData := C.CBytes(data)
	defer C.free(cData)

	pixels := C.stbi_load_from_memory((*C.uchar)(cData), C.int(len(data)), &w, &h, &channels, 4)
	if pixels == nil {
		return nil, fmt.Errorf("stb_image: decode failed")
	}
	defer C.stbi_image_free(unsafe.Pointer(pixels))

	width := int(w)
	height := int(h)
	stride := width * 4
	size := stride * height

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	copy(img.Pix, unsafe.Slice((*byte)(unsafe.Pointer(pixels)), size))
	return img, nil
}

// resizeImage uses x/image/draw for fast bilinear downscaling.
func resizeImage(img image.Image, maxDim int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		if nrgba, ok := img.(*image.NRGBA); ok {
			return nrgba
		}
		dst := image.NewNRGBA(b)
		draw.Copy(dst, image.Point{}, img, b, draw.Src, nil)
		return dst
	}
	scale := float64(maxDim) / float64(w)
	if h > w {
		scale = float64(maxDim) / float64(h)
	}
	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// CompressImage decodes image data (PNG/JPEG) via stb_image, downscales,
// and encodes to WebP. Returns the compressed bytes and content type.
func CompressImage(imgData []byte) ([]byte, string, error) {
	img, err := decodeImage(imgData)
	if err != nil {
		return nil, "", err
	}

	resized := resizeImage(img, maxDim)

	// Binary search for highest quality that fits under Threshold
	lo, hi := 10, 80
	var best []byte
	for lo <= hi {
		mid := (lo + hi) / 2
		var buf bytes.Buffer
		if err := webp.Encode(&buf, resized, &webp.Options{Quality: float32(mid)}); err != nil {
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
		return best, "image/webp", nil
	}
	var buf bytes.Buffer
	if err := webp.Encode(&buf, resized, &webp.Options{Quality: 10}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/webp", nil
}
