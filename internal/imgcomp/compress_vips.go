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

// CompressToJPEG compresses PNG data to JPEG, downscaling and adjusting quality
// to fit under Threshold bytes. Uses libvips for high performance.
func CompressToJPEG(pngData []byte) ([]byte, error) {
	initVips()

	img, err := vips.NewImageFromBuffer(pngData)
	if err != nil {
		return nil, err
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
			return nil, err
		}
	}

	// Binary search for highest quality that fits under Threshold
	lo, hi := 10, 80
	var best []byte
	for lo <= hi {
		mid := (lo + hi) / 2
		ep := vips.NewJpegExportParams()
		ep.Quality = mid
		buf, _, err := img.ExportJpeg(ep)
		if err != nil {
			return nil, err
		}
		if len(buf) <= Threshold {
			best = buf
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best != nil {
		return best, nil
	}
	// Even quality 10 is too large — return best effort
	ep := vips.NewJpegExportParams()
	ep.Quality = 10
	buf, _, err := img.ExportJpeg(ep)
	if err != nil {
		return nil, err
	}
	return buf, nil
}
