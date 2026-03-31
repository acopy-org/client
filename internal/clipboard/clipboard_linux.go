package clipboard

/*
#cgo LDFLAGS: -lX11 -lXfixes
#include <X11/Xlib.h>
#include <X11/extensions/Xfixes.h>

static Display *dpy = NULL;
static int xfixes_event_base = 0;
static long change_count = 0;
static int ready = 0;

static void ensure_init() {
    if (ready) return;
    ready = 1;
    dpy = XOpenDisplay(NULL);
    if (!dpy) return;
    int event_base, error_base;
    if (!XFixesQueryExtension(dpy, &event_base, &error_base)) {
        XCloseDisplay(dpy);
        dpy = NULL;
        return;
    }
    xfixes_event_base = event_base;
    Atom clip = XInternAtom(dpy, "CLIPBOARD", False);
    XFixesSelectSelectionInput(dpy, DefaultRootWindow(dpy), clip,
        XFixesSetSelectionOwnerNotifyMask |
        XFixesSelectionWindowDestroyNotifyMask |
        XFixesSelectionClientCloseNotifyMask);
}

static long get_change_count() {
    ensure_init();
    if (!dpy) return -1;
    while (XPending(dpy)) {
        XEvent event;
        XNextEvent(dpy, &event);
        if (event.type == xfixes_event_base + XFixesSelectionNotify) {
            change_count++;
        }
    }
    return change_count;
}
*/
import "C"
import (
	"bytes"
	"fmt"
	"os/exec"
)

// ChangeCount uses X11 XFixes to get notified of clipboard changes.
// Zero CPU cost — just drains pending X events. No process spawning, no data read.
func ChangeCount() int64 {
	return int64(C.get_change_count())
}

func Read() ([]byte, error) {
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		return nil, fmt.Errorf("xclip: %w", err)
	}
	return out, nil
}

func Write(data []byte) error {
	cmd := exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xclip: %w", err)
	}
	return nil
}
