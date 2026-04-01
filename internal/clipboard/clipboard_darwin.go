package clipboard

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"sync"
)

var (
	lastHash [sha256.Size]byte
	seqNo    int64
	mu       sync.Mutex
)

func ChangeCount() int64 {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return seqNo
	}

	h := sha256.Sum256(out)

	mu.Lock()
	defer mu.Unlock()
	if h != lastHash {
		lastHash = h
		seqNo++
	}
	return seqNo
}

func Read() ([]byte, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return nil, fmt.Errorf("pbpaste: %w", err)
	}
	return out, nil
}

func Write(data []byte) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pbcopy: %w", err)
	}
	return nil
}
