//go:build darwin && cgo

package clipboard

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

extern long clipboardChangeCount();
extern void* clipboardReadImage(int* outLen);
extern void* clipboardReadText(int* outLen);
extern void clipboardWriteImage(const void* data, int len);
extern void clipboardWriteImageAndText(const void* imgData, int imgLen, const char* text);
extern void clipboardWriteText(const char* text, int len);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// ChangeCount returns NSPasteboard's native change counter.
func ChangeCount() int64 {
	return int64(C.clipboardChangeCount())
}

// Read returns clipboard content and its MIME type.
func Read() ([]byte, string, error) {
	// Try image first
	var imgLen C.int
	imgPtr := C.clipboardReadImage(&imgLen)
	if imgPtr != nil {
		defer C.free(imgPtr)
		return C.GoBytes(imgPtr, imgLen), "image/png", nil
	}

	// Fall back to text
	var txtLen C.int
	txtPtr := C.clipboardReadText(&txtLen)
	if txtPtr != nil {
		defer C.free(txtPtr)
		return C.GoBytes(txtPtr, txtLen), "text/plain", nil
	}

	return nil, "", fmt.Errorf("clipboard empty")
}

// Write sets the clipboard content.
func Write(data []byte, contentType string, clipURL string) error {
	switch {
	case contentType == "image/png" && clipURL != "":
		cText := C.CString(clipURL)
		defer C.free(unsafe.Pointer(cText))
		C.clipboardWriteImageAndText(unsafe.Pointer(&data[0]), C.int(len(data)), cText)
	case contentType == "image/png":
		C.clipboardWriteImage(unsafe.Pointer(&data[0]), C.int(len(data)))
	default:
		cText := (*C.char)(unsafe.Pointer(&data[0]))
		C.clipboardWriteText(cText, C.int(len(data)))
	}
	return nil
}
