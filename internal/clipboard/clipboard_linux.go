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
		_, err := exec.LookPath("xclip")
		xclipFound = err == nil
		if !xclipFound {
			log.Printf("xclip not found — clipboard sync will save to ~/.cache/acopy/")
			log.Printf("install xclip for full clipboard support: %s", installHint())
		}
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
// Otherwise saves to ~/.cache/acopy/ as a fallback for headless SSH.
func Write(data []byte, contentType string, clipURL string) error {
	// For images with a URL, write the URL as text so Ctrl+V pastes it
	if strings.HasPrefix(contentType, "image/") && clipURL != "" {
		data = []byte(clipURL)
		contentType = "text/plain"
	}

	if hasXclip() {
		args := []string{"-selection", "clipboard"}
		if contentType == "image/png" {
			args = append(args, "-t", "image/png")
		}
		cmd := exec.Command("xclip", args...)
		cmd.Stdin = bytes.NewReader(data)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("xclip: %w", err)
		}
		return nil
	}

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

	// Update latest symlink
	latest := filepath.Join(dir, "latest."+ext)
	os.Remove(latest)
	os.Symlink(name, latest)

	log.Printf("saved clipboard to %s", path)
	return nil
}
