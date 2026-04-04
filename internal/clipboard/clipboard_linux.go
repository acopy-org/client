package clipboard

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	lastHash     [sha256.Size]byte
	seqNo        int64
	mu           sync.Mutex
	xclipChecked bool
	xclipFound   bool
)

func hasXclip() bool {
	if !xclipChecked {
		xclipChecked = true
		xclipPath, err := exec.LookPath("xclip")
		if err != nil {
			log.Printf("xclip not found — clipboard sync will save to ~/.cache/acopy/")
			log.Printf("install xclip for full clipboard support: %s", installHint())
			return false
		}
		// If the found xclip is our own shim (symlink to acopy), treat it
		// as no native clipboard.  Using the shim for writes would POST
		// back to the server and create a broadcast loop.
		if target, err := os.Readlink(xclipPath); err == nil {
			if filepath.Base(target) == "acopy" {
				log.Printf("xclip is acopy shim — clipboard writes will save to cache")
				return false
			}
		}
		// Verify xclip can actually connect to a display
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o").CombinedOutput()
		if err != nil && strings.Contains(string(out), "Can't open display") {
			log.Printf("xclip installed but no display available — clipboard writes will save to cache")
			log.Printf("set DISPLAY or run within a desktop session for full clipboard support")
			return false
		}
		xclipFound = true
	}
	return xclipFound
}

func installHint() string {
	if _, err := exec.LookPath("apt-get"); err == nil {
		return "sudo apt-get install -y xclip"
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return "sudo dnf install -y xclip"
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "sudo yum install -y xclip"
	}
	if _, err := exec.LookPath("pacman"); err == nil {
		return "sudo pacman -S xclip"
	}
	if _, err := exec.LookPath("zypper"); err == nil {
		return "sudo zypper install xclip"
	}
	if _, err := exec.LookPath("apk"); err == nil {
		return "sudo apk add xclip"
	}
	return "install xclip using your package manager"
}

// HasNativeClipboard reports whether a system clipboard is available.
func HasNativeClipboard() bool {
	return hasXclip()
}

// ChangeCount detects clipboard changes by hashing the available
// TARGETS plus content. This catches both text and image changes.
func ChangeCount() int64 {
	if !hasXclip() {
		return seqNo
	}

	h := sha256.New()

	targets, _ := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "TARGETS").Output()
	h.Write(targets)

	if bytes.Contains(targets, []byte("image/png")) {
		img, _ := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "image/png").Output()
		h.Write(img)
	} else {
		text, _ := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
		h.Write(text)
	}

	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))

	mu.Lock()
	defer mu.Unlock()
	if sum != lastHash {
		lastHash = sum
		seqNo++
	}
	return seqNo
}

// Read returns clipboard content and its MIME type.
func Read() ([]byte, string, error) {
	targets, _ := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "TARGETS").Output()

	if bytes.Contains(targets, []byte("image/png")) {
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "image/png").Output()
		if err == nil && len(out) > 0 {
			return out, "image/png", nil
		}
	}

	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		return nil, "", fmt.Errorf("xclip: %w", err)
	}
	return out, "text/plain", nil
}

// Write sets the clipboard content. If xclip is available, uses it.
// Otherwise falls back to a cache file.
func Write(data []byte, contentType string, clipURL string) error {
	if hasXclip() {
		// For images, always put a pasteable string in the clipboard:
		// the URL if available, otherwise save to cache and use the file path.
		if strings.HasPrefix(contentType, "image/") {
			if clipURL != "" {
				data = []byte(clipURL)
			} else {
				if err := saveToCache(data, contentType); err != nil {
					return err
				}
				home, _ := os.UserHomeDir()
				data = []byte(filepath.Join(home, ".cache", "acopy", "latest"))
			}
			contentType = "text/plain"
		}
		args := []string{"-selection", "clipboard"}
		cmd := exec.Command("xclip", args...)
		cmd.Stdin = bytes.NewReader(data)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			detail := strings.TrimSpace(stderr.String())
			if detail != "" {
				return fmt.Errorf("xclip: %w (%s)", err, detail)
			}
			return fmt.Errorf("xclip: %w", err)
		}
		return nil
	}

	// No display — save to cache.  Write() is only called for remote
	// clipboard updates.  OSC 52 would travel back through SSH and set the
	// clipboard on the terminal host, which already has acopy running.
	return saveToCache(data, contentType)
}

func saveToCache(data []byte, contentType string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("user home: %w", err)
	}
	dir := filepath.Join(home, ".cache", "acopy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir cache: %w", err)
	}

	ext := "txt"
	switch contentType {
	case "image/png":
		ext = "png"
	case "image/jpeg":
		ext = "jpg"
	}

	ts := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("clip-%s.%s", ts, ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}

	// Update type-specific and universal latest symlinks
	latest := filepath.Join(dir, "latest."+ext)
	os.Remove(latest)
	os.Symlink(name, latest)

	latestAny := filepath.Join(dir, "latest")
	os.Remove(latestAny)
	os.Symlink(name, latestAny)

	log.Printf("saved clipboard to %s", path)
	return nil
}
