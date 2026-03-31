package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const plistName = "com.acopy.client"

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{ .Label }}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{ .BinPath }}</string>
        <string>start</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{ .LogDir }}/acopy.log</string>
    <key>StandardErrorPath</key>
    <string>{{ .LogDir }}/acopy.err</string>
    <key>ProcessType</key>
    <string>Background</string>
    <key>LowPriorityBackgroundIO</key>
    <true/>
</dict>
</plist>
`))

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistName+".plist")
}

func logDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Logs", "acopy")
	os.MkdirAll(dir, 0o755)
	return dir
}

func Setup(binPath string) error {
	abs, err := filepath.Abs(binPath)
	if err != nil {
		return err
	}

	// Copy binary to a stable location
	installDir := filepath.Join("/usr/local/bin")
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

	path := plistPath()
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer f.Close()

	err = plistTmpl.Execute(f, struct {
		Label   string
		BinPath string
		LogDir  string
	}{
		Label:   plistName,
		BinPath: abs,
		LogDir:  logDir(),
	})
	if err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}
	return nil
}

func Remove() error {
	path := plistPath()
	_ = exec.Command("launchctl", "unload", path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

func Status() (string, error) {
	out, err := exec.Command("launchctl", "list", plistName).CombinedOutput()
	if err != nil {
		return "not installed", nil
	}
	return string(out), nil
}
