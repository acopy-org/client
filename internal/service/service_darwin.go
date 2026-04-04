package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"time"
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
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>{{ .Home }}</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{ .LogDir }}/acopy.log</string>
    <key>StandardErrorPath</key>
    <string>{{ .LogDir }}/acopy.err</string>
    <key>ProcessType</key>
    <string>Adaptive</string>
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

	// Copy binary to a stable location in user's home
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
		Home    string
	}{
		Label:   plistName,
		BinPath: abs,
		LogDir:  logDir(),
		Home:    home,
	})
	if err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	uid := os.Getuid()
	domain := fmt.Sprintf("gui/%d", uid)
	target := fmt.Sprintf("gui/%d/%s", uid, plistName)

	// Remove any previously loaded service
	_ = exec.Command("launchctl", "bootout", target).Run()
	time.Sleep(500 * time.Millisecond)

	// Bootstrap (load + start) using modern launchctl
	if out, err := exec.Command("launchctl", "bootstrap", domain, path).CombinedOutput(); err != nil {
		// Fallback to legacy load for older macOS
		if out2, err2 := exec.Command("launchctl", "load", path).CombinedOutput(); err2 != nil {
			return fmt.Errorf("launchctl bootstrap: %s; load: %s", out, out2)
		}
	}

	// Force-start the service to ensure it's running now
	_ = exec.Command("launchctl", "kickstart", "-k", target).Run()

	return nil
}

func Stop() error {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), plistName)
	if err := exec.Command("launchctl", "bootout", target).Run(); err != nil {
		// Fallback to legacy unload
		path := plistPath()
		if err2 := exec.Command("launchctl", "unload", path).Run(); err2 != nil {
			return fmt.Errorf("stop service: %w", err2)
		}
	}
	return nil
}

func Remove() error {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), plistName)
	_ = exec.Command("launchctl", "bootout", target).Run()
	path := plistPath()
	_ = exec.Command("launchctl", "unload", path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	// Remove installed binary
	home, _ := os.UserHomeDir()
	binPath := filepath.Join(home, ".local", "bin", "acopy")
	if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove binary: %w", err)
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
