package clipboard

import (
	"bytes"
	"fmt"
	"os/exec"
	"syscall"
)

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	getClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
)

// HasNativeClipboard reports whether a system clipboard is available.
func HasNativeClipboard() bool { return true }

// ChangeCount calls Win32 GetClipboardSequenceNumber.
// Returns a counter that increments on every clipboard change.
// Single syscall, no process spawn, no data read.
func ChangeCount() int64 {
	ret, _, _ := getClipboardSequenceNumber.Call()
	return int64(ret)
}

func Read() ([]byte, string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw").Output()
	if err != nil {
		return nil, "", fmt.Errorf("Get-Clipboard: %w", err)
	}
	return out, "text/plain", nil
}

func Write(data []byte, contentType string, clipURL string) error {
	if contentType == "image/png" && clipURL != "" {
		data = []byte(clipURL)
	}
	cmd := exec.Command("clip.exe")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clip: %w", err)
	}
	return nil
}
