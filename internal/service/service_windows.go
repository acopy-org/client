package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const taskName = "acopy"

func installDir() string {
	local := os.Getenv("LOCALAPPDATA")
	dir := filepath.Join(local, "acopy")
	os.MkdirAll(dir, 0o755)
	return dir
}

func Setup(binPath string) error {
	abs, err := filepath.Abs(binPath)
	if err != nil {
		return err
	}

	// Copy binary to a stable location
	dest := filepath.Join(installDir(), "acopy.exe")
	if abs != dest {
		src, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read binary: %w", err)
		}
		if err := os.WriteFile(dest, src, 0o755); err != nil {
			return fmt.Errorf("install binary to %s: %w", dest, err)
		}
		abs = dest
	}

	// Remove existing task if any
	_ = exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()

	// Create scheduled task that runs at logon
	err = exec.Command("schtasks", "/Create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s" start`, abs),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	).Run()
	if err != nil {
		return fmt.Errorf("schtasks create: %w", err)
	}

	// Start it now
	if err := exec.Command("schtasks", "/Run", "/TN", taskName).Run(); err != nil {
		return fmt.Errorf("schtasks run: %w", err)
	}
	return nil
}

func Remove() error {
	if err := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run(); err != nil {
		return fmt.Errorf("schtasks delete: %w", err)
	}

	// Remove installed binary
	binPath := filepath.Join(installDir(), "acopy.exe")
	if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove %s: permission denied", binPath)
	}
	return nil
}

func Status() (string, error) {
	out, err := exec.Command("schtasks", "/Query", "/TN", taskName, "/FO", "LIST").CombinedOutput()
	if err != nil {
		return "not installed", nil
	}
	return string(out), nil
}
