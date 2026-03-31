package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

long getChangeCount() {
    return [[NSPasteboard generalPasteboard] changeCount];
}
*/
import "C"
import (
	"bytes"
	"fmt"
	"os/exec"
)

func ChangeCount() int64 {
	return int64(C.getChangeCount())
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
