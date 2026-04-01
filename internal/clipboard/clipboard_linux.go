package clipboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
		if err != nil {
			log.Printf("xclip not found — clipboard sync will save to ~/.cache/acopy/")
			log.Printf("install xclip for full clipboard support: %s", installHint())
			return false
		}
		// Verify xclip can actually connect to a display
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o").CombinedOutput()
		if err != nil && strings.Contains(string(out), "Can't open display") {
			log.Printf("xclip installed but no display available — will use OSC 52 for text, cache for images")
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
// Otherwise tries OSC 52 escape sequence, then falls back to cache file.
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

	// No display — try OSC 52 (text only, most terminals cap at ~100KB)
	if contentType == "text/plain" {
		if err := writeOSC52(data); err == nil {
			return nil
		}
	}

	return saveToCache(data, contentType)
}

// writeOSC52 writes the OSC 52 escape sequence to all of the current user's
// active PTY sessions. The terminal emulator on the other end (iTerm2, kitty,
// alacritty, etc.) interprets it and sets its local clipboard.
func writeOSC52(data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	// \033]52;c;<base64>\a
	seq := fmt.Sprintf("\033]52;c;%s\a", encoded)

	// Try our own controlling terminal first
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		tty.WriteString(seq)
		tty.Close()
		return nil
	}

	// Find PTYs owned by current user (for systemd service / detached sessions)
	uid := os.Getuid()
	entries, err := os.ReadDir("/dev/pts")
	if err != nil {
		return fmt.Errorf("no terminals available")
	}

	wrote := false
	for _, e := range entries {
		if e.Name() == "ptmx" {
			continue
		}
		// Only numeric entries are PTY slaves
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		path := filepath.Join("/dev/pts", e.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		if int(stat.Uid) != uid {
			continue
		}
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			continue
		}
		f.WriteString(seq)
		f.Close()
		wrote = true
	}

	if !wrote {
		return fmt.Errorf("no terminals available for OSC 52")
	}
	return nil
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
