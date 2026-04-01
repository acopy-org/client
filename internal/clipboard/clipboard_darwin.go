package clipboard

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ChangeCount returns the NSPasteboard change count, which increments
// on every clipboard change (text, images, files, etc.).
func ChangeCount() int64 {
	out, err := exec.Command("osascript", "-l", "JavaScript", "-e",
		`ObjC.import('AppKit'); $.NSPasteboard.generalPasteboard.changeCount`).Output()
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// readImageScript checks for image data on the pasteboard and returns
// it as base64-encoded PNG. Returns empty string if no image is found.
const readImageScript = `ObjC.import('AppKit');
ObjC.import('Foundation');
var pb = $.NSPasteboard.generalPasteboard;
var types = ObjC.deepUnwrap(pb.types);
var imageTypes = ['public.png', 'public.tiff', 'public.jpeg'];
if (!imageTypes.some(function(t){ return types.indexOf(t) >= 0; })) {
  '';
} else {
  var img = $.NSImage.alloc.initWithPasteboard(pb);
  if (img.isNil()) {
    '';
  } else {
    var tiff = img.TIFFRepresentation;
    var bmp = $.NSBitmapImageRep.imageRepWithData(tiff);
    var png = bmp.representationUsingTypeProperties($.NSBitmapImageFileTypePNG, $());
    png.base64EncodedStringWithOptions(0).js;
  }
}`

// Read returns clipboard content and its MIME type.
// Images are returned as PNG bytes with "image/png".
// Text is returned with "text/plain".
func Read() ([]byte, string, error) {
	out, err := exec.Command("osascript", "-l", "JavaScript", "-e", readImageScript).Output()
	if err == nil {
		b64 := strings.TrimSpace(string(out))
		if b64 != "" {
			data, err := base64.StdEncoding.DecodeString(b64)
			if err == nil && len(data) > 0 {
				return data, "image/png", nil
			}
		}
	}

	out, err = exec.Command("pbpaste").Output()
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
	cmd := exec.Command("pbcopy")
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
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("osascript set clipboard: %w", err)
	}
	return nil
}

func writeImageWithURL(data []byte, url string) error {
	if _, err := exec.LookPath("swift"); err != nil {
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

	cmd := exec.Command("swift", "-e", swift)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("swift clipboard: %w: %s", err, out)
	}
	return nil
}
