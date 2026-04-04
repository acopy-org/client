//go:build darwin && !cgo

package clipboard

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

var (
	lastHash [sha256.Size]byte
	seqNo    int64
	mu       sync.Mutex
)

// HasNativeClipboard reports whether a system clipboard is available.
func HasNativeClipboard() bool { return true }

// ChangeCount detects clipboard changes by hashing clipboard types only.
// IMPORTANT: We only hash clipboard TYPES, not content, because reading image
// content via AppleScript modifies clipboard state and causes double-push.
// The actual content deduplication is handled by the monitor's lastWasRemote flag.
func ChangeCount() int64 {
	mu.Lock()
	defer mu.Unlock()

	h := sha256.New()

	// Hash ONLY the types list - do NOT read actual content here!
	// Reading PNG from clipboard via AppleScript modifies clipboard state.
	types, _ := exec.Command("/usr/bin/osascript", "-e", "clipboard info").Output()
	h.Write(types)

	// Also hash current text content (pbpaste doesn't modify clipboard)
	if text, err := exec.Command("/usr/bin/pbpaste").Output(); err == nil {
		h.Write(text)
	}

	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))

	if sum != lastHash {
		lastHash = sum
		seqNo++
	}
	return seqNo
}

// Read returns clipboard content and its MIME type.
// Images are returned as PNG bytes with "image/png".
// Text is returned with "text/plain".
func Read() ([]byte, string, error) {
	// Try reading image via AppleScript (handles PNG, TIFF, JPEG — macOS converts automatically)
	f, err := os.CreateTemp("", "acopy-read-*.png")
	if err == nil {
		tmpPath := f.Name()
		f.Close()
		defer os.Remove(tmpPath)

		script := fmt.Sprintf(
			`set theImage to the clipboard as «class PNGf»
set theFile to open for access POSIX file %q with write permission
write theImage to theFile
close access theFile`, tmpPath)

		if err := exec.Command("/usr/bin/osascript", "-e", script).Run(); err == nil {
			data, err := os.ReadFile(tmpPath)
			if err == nil && len(data) > 0 {
				return data, "image/png", nil
			}
		}
	}

	// Fall back to text
	out, err := exec.Command("/usr/bin/pbpaste").Output()
	if err != nil {
		return nil, "", fmt.Errorf("pbpaste: %w", err)
	}
	return out, "text/plain", nil
}

// Write sets the clipboard content. For images with a clipURL, it writes
// both the image and the URL to the clipboard (multi-type) so terminal apps
// paste the URL and image apps paste the image. For text, it uses pbcopy.
func Write(data []byte, contentType string, clipURL string) error {
	if contentType == "image/png" {
		if clipURL != "" {
			return writeImageWithURL(data, clipURL)
		}
		return writeImage(data)
	}
	cmd := exec.Command("/usr/bin/pbcopy")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pbcopy: %w", err)
	}
	return nil
}

func writeImage(data []byte) error {
	f, err := os.CreateTemp("", "acopy-*.png")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	f.Close()

	script := fmt.Sprintf(`set the clipboard to (read POSIX file %q as «class PNGf»)`, f.Name())
	if err := exec.Command("/usr/bin/osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("osascript set clipboard: %w", err)
	}
	return nil
}

func writeImageWithURL(data []byte, url string) error {
	if _, err := exec.LookPath("/usr/bin/swift"); err != nil {
		return writeImage(data)
	}

	f, err := os.CreateTemp("", "acopy-*.png")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	f.Close()

	swift := fmt.Sprintf(`
import AppKit
let pb = NSPasteboard.general
pb.clearContents()
let imgData = try! Data(contentsOf: URL(fileURLWithPath: %q))
pb.setData(imgData, forType: .png)
pb.setString(%q, forType: .string)
`, f.Name(), url)

	cmd := exec.Command("/usr/bin/swift", "-e", swift)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("swift clipboard: %w: %s", err, out)
	}
	return nil
}
