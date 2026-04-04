package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/riz/acopy-client/internal/auth"
	"github.com/riz/acopy-client/internal/config"
	"github.com/riz/acopy-client/internal/monitor"
	"github.com/riz/acopy-client/internal/service"
	acSync "github.com/riz/acopy-client/internal/sync"
)

// Version is set at build time via -ldflags
var Version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("acopy: ")

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		cmdStart()
	case "setup":
		cmdSetup()
	case "stop":
		cmdStop()
	case "remove":
		cmdRemove()
	case "status":
		cmdStatus()
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
  start       Start clipboard sync (foreground)
  version     Show version`)
}

func cmdStart() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.ServerURL = "https://acopy.org"
	if cfg.Token == "" {
		fmt.Println("not configured, running setup...")
		loginSetup(cfg)
	}
	if cfg.DeviceName == "" {
		cfg.DeviceName, _ = os.Hostname()
	}

	client, err := acSync.NewClient(cfg.ServerURL, cfg.Token, cfg.DeviceName)
	if err != nil {
		log.Fatalf("init client: %v", err)
	}

	mon := monitor.New(client, cfg.DeviceName, cfg.ServerURL)

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

	// Prompt for device name
	if cfg.DeviceName == "" {
		hostname, _ := os.Hostname()
		cfg.DeviceName = prompt(fmt.Sprintf("Device name [%s]", hostname))
		if cfg.DeviceName == "" {
			cfg.DeviceName = hostname
		}
	}

	// Auth
	if cfg.Token == "" {
		choice := prompt("Login or register? [l/r]")
		creds := auth.Credentials{
			Email:    prompt("Email"),
			Password: promptPassword("Password"),
		}
		if strings.HasPrefix(strings.ToLower(choice), "r") {
			if err := auth.Register(cfg.ServerURL, creds); err != nil {
				log.Fatalf("register: %v", err)
			}
			fmt.Println("registered successfully")
		}
		if err := auth.Login(cfg, creds); err != nil {
			log.Fatalf("login: %v", err)
		}
		fmt.Println("logged in")
	}
}

func cmdSetup() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

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

	// Clear config (logout)
	cfg, err := config.Load()
	if err == nil && cfg.Token != "" {
		cfg.Token = ""
		cfg.Save()
	}

	fmt.Println("service stopped, logged out, and removed")
}

func cmdStatus() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
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

func prompt(label string) string {
	fmt.Printf("%s: ", label)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func promptPassword(label string) string {
	fmt.Printf("%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		log.Fatalf("read password: %v", err)
	}
	return strings.TrimSpace(string(b))
}
