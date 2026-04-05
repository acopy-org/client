package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/riz/acopy-client/internal/auth"
	"github.com/riz/acopy-client/internal/clipboard"
	"github.com/riz/acopy-client/internal/config"
	"github.com/riz/acopy-client/internal/monitor"
	"github.com/riz/acopy-client/internal/service"
	acSync "github.com/riz/acopy-client/internal/sync"
)

// Version is set at build time via -ldflags
var Version = "dev"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lmsgprefix)
	log.SetPrefix("acopy: ")

	// Symlink mode: act as xclip/xsel/pbcopy/pbpaste
	switch filepath.Base(os.Args[0]) {
	case "xclip":
		shimXclip()
		return
	case "xsel":
		shimXsel()
		return
	case "pbcopy":
		cmdCopy()
		return
	case "pbpaste":
		cmdPaste()
		return
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		debug := len(os.Args) > 2 && os.Args[2] == "debug"
		cmdStart(debug)
	case "setup":
		cmdSetup()
	case "stop":
		cmdStop()
	case "remove":
		cmdRemove()
	case "status":
		cmdStatus()
	case "copy":
		cmdCopy()
	case "paste":
		cmdPaste()
	case "install-shims":
		cmdInstallShims()
	case "version", "--version", "-v":
		fmt.Println("acopy " + Version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: acopy <command>

commands:
  setup       Register/login + install as system service
  stop        Stop the service
  remove      Remove system service
  status      Show config and service status
  copy          Push stdin to clipboard (e.g. echo hi | acopy copy)
  paste         Output latest clipboard to stdout
  install-shims Create xclip/xsel/pbcopy/pbpaste symlinks
  start         Start clipboard sync (foreground)
  start debug   Start with verbose timestamped logging
  version       Show version`)
}

func cmdStart(debug bool) {
	if debug {
		log.Printf("starting in debug mode (version %s)", Version)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.ServerURL = "https://acopy.org"
	if cfg.Token == "" {
		fmt.Println("not configured, running setup...")
		promptDeviceName(cfg)
		fmt.Println()
		loginSetup(cfg)
	}
	if cfg.DeviceName == "" {
		cfg.DeviceName, _ = os.Hostname()
	}

	if debug {
		log.Printf("config: server=%s device=%s token=%s...", cfg.ServerURL, cfg.DeviceName, cfg.Token[:min(8, len(cfg.Token))])
	}

	client, err := acSync.NewClient(cfg.ServerURL, cfg.Token, cfg.DeviceName)
	if err != nil {
		log.Fatalf("init client: %v", err)
	}

	mon := monitor.New(client, cfg.DeviceName, cfg.ServerURL)
	if debug {
		mon.Debug = true
	}

	client.OnDeviceId = func(deviceID string) {
		if cfg.DeviceID != deviceID {
			cfg.DeviceID = deviceID
			if err := cfg.Save(); err != nil {
				log.Printf("save config after device id: %v", err)
			}
		}
	}

	client.OnDeviceRenamed = func(oldName, newName string) {
		if oldName == cfg.DeviceName {
			cfg.DeviceName = newName
			mon.SetDevice(newName)
			if err := cfg.Save(); err != nil {
				log.Printf("save config after rename: %v", err)
			}
			log.Printf("device renamed: %s -> %s", oldName, newName)
		}
	}

	client.OnDeviceDeleted = func(deviceID string) {
		log.Printf("a device was removed (id: %s)", deviceID)
	}

	// Handle shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("shutting down")
		mon.Stop()
		client.Stop()
		os.Exit(0)
	}()

	// Start WebSocket connection in background
	go client.Run()

	log.Printf("syncing clipboard as %s", cfg.DeviceName)
	mon.Run()
}

func loginSetup(cfg *config.Config) {
	cfg.ServerURL = "https://acopy.org"

	// Auth
	if cfg.Token == "" {
		fmt.Println("New users will be automatically registered.")
		creds := auth.Credentials{
			Email:    prompt("Email"),
			Password: promptPassword("Password"),
		}
		// Try login first, register if account doesn't exist
		if err := auth.Login(cfg, creds); err != nil {
			// New user — confirm password before registering
			confirm := promptPassword("Confirm password")
			if confirm != creds.Password {
				log.Fatalf("passwords do not match")
			}
			if regErr := auth.Register(cfg.ServerURL, creds); regErr != nil {
				log.Fatalf("registration failed: %v", regErr)
			}
			fmt.Println("registered successfully")
			if err := auth.Login(cfg, creds); err != nil {
				log.Fatalf("login: %v", err)
			}
		}
		fmt.Println("logged in")
	}
}

func promptDeviceName(cfg *config.Config) {
	hostname, _ := os.Hostname()
	current := cfg.DeviceName
	if current == "" {
		current = hostname
	}
	fmt.Printf("Device name: %s\n", current)
	override := prompt("Press enter to confirm, or type a new name")
	if override != "" {
		cfg.DeviceName = override
	} else if cfg.DeviceName == "" {
		cfg.DeviceName = hostname
	}
}

func cmdSetup() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	promptDeviceName(cfg)
	fmt.Println()
	loginSetup(cfg)

	// Install service
	bin, err := os.Executable()
	if err != nil {
		log.Fatalf("resolve binary path: %v", err)
	}
	if err := service.Setup(bin); err != nil {
		log.Fatalf("setup: %v", err)
	}
	fmt.Println("service installed and started")

	// Install clipboard shims on headless systems
	if !clipboard.HasNativeClipboard() {
		fmt.Println()
		fmt.Println("no display detected — installing clipboard shims...")
		cmdInstallShims()
	}
}

func cmdStop() {
	if err := service.Stop(); err != nil {
		log.Fatalf("stop: %v", err)
	}
	fmt.Println("service stopped")
}

func cmdRemove() {
	if err := service.Remove(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// Clear config (logout + device name)
	cfg, err := config.Load()
	if err == nil {
		cfg.Token = ""
		cfg.DeviceName = ""
		cfg.Save()
	}

	// Remove clipboard shim symlinks if they point to acopy
	removeShims()

	fmt.Println("service stopped, logged out, and removed")
}

func cmdStatus() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	fmt.Printf("version: %s\n", Version)
	fmt.Printf("server:  %s\n", cfg.ServerURL)
	fmt.Printf("device:  %s\n", cfg.DeviceName)
	if cfg.Token != "" {
		fmt.Println("auth:    logged in")
	} else {
		fmt.Println("auth:    not logged in")
	}

	svc, _ := service.Status()
	fmt.Printf("service: %s\n", svc)
}

func cmdCopy() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.ServerURL = "https://acopy.org"
	if cfg.Token == "" {
		log.Fatalf("not configured — run: acopy setup")
	}
	if cfg.DeviceName == "" {
		cfg.DeviceName, _ = os.Hostname()
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("read stdin: %v", err)
	}
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		log.Fatalf("empty input")
	}

	contentType := "text/plain"
	if len(data) > 8 && string(data[:4]) == "\x89PNG" {
		contentType = "image/png"
	}

	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/clipboard/push", bytes.NewReader(data))
	if err != nil {
		log.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("X-Acopy-Device", cfg.DeviceName)
	req.Header.Set("X-Acopy-Content-Type", contentType)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("push: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("push failed: %d %s", resp.StatusCode, body)
	}
}

func cmdPaste() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("user home: %v", err)
	}

	latest := filepath.Join(home, ".cache", "acopy", "latest")
	target, err := os.Readlink(latest)
	if err != nil {
		log.Fatalf("no clipboard content available")
	}

	path := target
	if !filepath.IsAbs(target) {
		path = filepath.Join(home, ".cache", "acopy", target)
	}

	ext := filepath.Ext(path)
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
		// Output the file path for images — pasting binary into a
		// terminal is never useful. Tools can read the file directly.
		fmt.Print(path)
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read clipboard: %v", err)
	}

	os.Stdout.Write(data)
}

func shimTargets() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	latest := filepath.Join(home, ".cache", "acopy", "latest")
	target, err := os.Readlink(latest)
	if err != nil {
		return
	}
	ext := filepath.Ext(target)
	switch ext {
	case ".png":
		fmt.Println("image/png")
	case ".jpg", ".jpeg":
		fmt.Println("image/jpeg")
	default:
		fmt.Println("UTF8_STRING")
	}
}

func shimXclip() {
	isOutput := false
	targetType := ""
	for i, arg := range os.Args[1:] {
		if arg == "-o" {
			isOutput = true
		}
		if arg == "-t" && i+1 < len(os.Args)-1 {
			targetType = os.Args[i+2]
		}
	}
	if isOutput {
		if targetType == "TARGETS" {
			shimTargets()
			return
		}
		if targetType == "image/png" || targetType == "image/jpeg" {
			// Explicit image type request — return binary data
			shimPasteImage(targetType)
			return
		}
		// Default: return text-friendly output (path for images)
		cmdPaste()
		return
	}
	cmdCopy()
}

func shimPasteImage(mime string) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("user home: %v", err)
	}
	latest := filepath.Join(home, ".cache", "acopy", "latest")
	target, err := os.Readlink(latest)
	if err != nil {
		os.Exit(1)
	}
	path := target
	if !filepath.IsAbs(target) {
		path = filepath.Join(home, ".cache", "acopy", target)
	}
	ext := filepath.Ext(path)
	// Only serve if the cached content matches the requested type
	switch mime {
	case "image/png":
		if ext != ".png" {
			os.Exit(1)
		}
	case "image/jpeg":
		if ext != ".jpg" && ext != ".jpeg" {
			os.Exit(1)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		os.Exit(1)
	}
	os.Stdout.Write(data)
}

func shimXsel() {
	for _, arg := range os.Args[1:] {
		if arg == "-o" || arg == "--output" {
			cmdPaste()
			return
		}
	}
	// xsel with no flags also outputs, unlike xclip
	hasInput := false
	for _, arg := range os.Args[1:] {
		if arg == "-i" || arg == "--input" {
			hasInput = true
			break
		}
	}
	if !hasInput && len(os.Args) > 1 {
		cmdPaste()
		return
	}
	cmdCopy()
}

func cmdInstallShims() {
	acopyBin, err := exec.LookPath("acopy")
	if err != nil {
		// Fall back to our own executable path
		acopyBin, err = os.Executable()
		if err != nil {
			log.Fatalf("resolve acopy path: %v", err)
		}
	}
	acopyBin, _ = filepath.Abs(acopyBin)

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("user home: %v", err)
	}
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	for _, name := range []string{"xclip", "xsel", "pbcopy", "pbpaste"} {
		link := filepath.Join(dir, name)
		os.Remove(link)
		if err := os.Symlink(acopyBin, link); err != nil {
			log.Printf("symlink %s: %v", name, err)
		} else {
			fmt.Printf("  %s -> %s\n", link, acopyBin)
		}
	}

	// Check if ~/.local/bin is in PATH
	pathDirs := filepath.SplitList(os.Getenv("PATH"))
	found := false
	for _, d := range pathDirs {
		if d == dir {
			found = true
			break
		}
	}
	if !found {
		fmt.Printf("\nadd to your shell profile:\n  export PATH=\"%s:$PATH\"\n", dir)
	}
}

func removeShims() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	acopyBin, _ := os.Executable()
	acopyBin, _ = filepath.Abs(acopyBin)

	dir := filepath.Join(home, ".local", "bin")
	for _, name := range []string{"xclip", "xsel", "pbcopy", "pbpaste"} {
		link := filepath.Join(dir, name)
		target, err := os.Readlink(link)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		if target == acopyBin {
			os.Remove(link)
			fmt.Printf("  removed shim %s\n", link)
		}
	}
}

func prompt(label string) string {
	fmt.Printf("%s: ", label)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func promptPassword(label string) string {
	fmt.Printf("%s: ", label)
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		log.Fatalf("terminal raw mode: %v", err)
	}
	defer term.Restore(fd, oldState)

	var pw []byte
	buf := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			log.Fatalf("read password: %v", err)
		}
		switch buf[0] {
		case '\r', '\n':
			fmt.Print("\r\n")
			return strings.TrimSpace(string(pw))
		case 3: // Ctrl-C
			fmt.Print("\r\n")
			os.Exit(1)
			return ""
		case 127, 8: // backspace, delete
			if len(pw) > 0 {
				pw = pw[:len(pw)-1]
				fmt.Print("\b \b")
			}
		default:
			pw = append(pw, buf[0])
			fmt.Print("●")
		}
	}
}
