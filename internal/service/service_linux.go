package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const unitName = "acopy.service"

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=acopy clipboard sync
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{ .BinPath }} start
Restart=on-failure
RestartSec=5
Environment=DISPLAY=:0

[Install]
WantedBy=default.target
`))

func unitDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "systemd", "user")
	os.MkdirAll(dir, 0o755)
	return dir
}

func unitPath() string {
	return filepath.Join(unitDir(), unitName)
}

func Setup(binPath string) error {
	abs, err := filepath.Abs(binPath)
	if err != nil {
		return err
	}

	// Copy binary to a stable location
	home, _ := os.UserHomeDir()
	installDir := filepath.Join(home, ".local", "bin")
	os.MkdirAll(installDir, 0o755)
	installPath := filepath.Join(installDir, "acopy")
	if abs != installPath {
		src, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read binary: %w", err)
		}
		if err := os.WriteFile(installPath, src, 0o755); err != nil {
			return fmt.Errorf("install binary to %s: %w", installPath, err)
		}
		abs = installPath
	}

	f, err := os.Create(unitPath())
	if err != nil {
		return fmt.Errorf("create unit: %w", err)
	}
	defer f.Close()

	if err := unitTmpl.Execute(f, struct{ BinPath string }{BinPath: abs}); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", unitName).Run(); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}
	return nil
}

func Stop() error {
	if err := exec.Command("systemctl", "--user", "stop", unitName).Run(); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

func Remove() error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", unitName).Run()
	if err := os.Remove(unitPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit: %w", err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	// Remove installed binary
	home, _ := os.UserHomeDir()
	if home != "" {
		binPath := filepath.Join(home, ".local", "bin", "acopy")
		if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove %s: permission denied", binPath)
		}
	}
	return nil
}

func Status() (string, error) {
	out, err := exec.Command("systemctl", "--user", "status", unitName).CombinedOutput()
	if err != nil {
		return "not installed", nil
	}
	return string(out), nil
}
