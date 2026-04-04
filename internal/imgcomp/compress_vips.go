//go:build vips

package imgcomp

import (
	"sync"

	"github.com/davidbyttow/govips/v2/vips"
)

var vipsOnce sync.Once

func initVips() {
	vipsOnce.Do(func() {
		vips.LoggingSettings(nil, vips.LogLevelWarning)
		vips.Startup(nil)
	})
}

// CompressImage compresses image data to WebP, downscaling and adjusting quality
// to fit under Threshold bytes. Uses libvips for high performance.
// Returns the compressed bytes and the output content type.
func CompressImage(imgData []byte) ([]byte, string, error) {
	initVips()

	img, err := vips.NewImageFromBuffer(imgData)
	if err != nil {
		return nil, "", err
	}
	defer img.Close()

	// Downscale if larger than maxDim
	w, h := img.Width(), img.Height()
	if w > maxDim || h > maxDim {
		scale := float64(maxDim) / float64(w)
		if h > w {
			scale = float64(maxDim) / float64(h)
		}
		if err := img.Resize(scale, vips.KernelLanczos3); err != nil {
			return nil, "", err
		}
	}

	// Binary search for highest quality that fits under Threshold
	lo, hi := 10, 80
	var best []byte
	for lo <= hi {
		mid := (lo + hi) / 2
		ep := vips.NewWebpExportParams()
		ep.Quality = mid
		buf, _, err := img.ExportWebp(ep)
		if err != nil {
			return nil, "", err
		}
		if len(buf) <= Threshold {
			best = buf
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best != nil {
		return best, "image/webp", nil
	}
	// Even quality 10 is too large — return best effort
	ep := vips.NewWebpExportParams()
	ep.Quality = 10
	buf, _, err := img.ExportWebp(ep)
	if err != nil {
		return nil, "", err
	}
	return buf, "image/webp", nil
}
