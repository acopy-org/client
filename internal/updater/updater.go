package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	checkInterval = 6 * time.Hour
	repoAPI       = "https://api.github.com/repos/acopy-org/client/releases/latest"
	repoDownload  = "https://github.com/acopy-org/client/releases/latest/download"

	// ExitCodeUpdate is the exit code used to signal the service manager
	// that the process should be restarted after a self-update.
	ExitCodeUpdate = 42
)

type releaseInfo struct {
	TagName string `json:"tag_name"`
}

// assetName returns the release asset filename for the current platform.
func assetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	name := fmt.Sprintf("acopy-%s-%s", os, arch)
	if os == "windows" {
		name += ".exe"
	}
	return name
}

// CheckForUpdate queries the GitHub Releases API and returns the latest
// version tag if it differs from currentVersion, or "" if up to date.
func CheckForUpdate(currentVersion string) (string, error) {
	req, err := http.NewRequest("GET", repoAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("check update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("github api: %s", resp.Status)
	}

	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("decode release: %w", err)
	}

	if rel.TagName == "" || rel.TagName == currentVersion {
		return "", nil
	}

	return rel.TagName, nil
}

// DownloadAndReplace downloads the new binary, verifies its checksum,
// and atomically replaces the current executable.
func DownloadAndReplace() error {
	asset := assetName()

	// Download checksums
	checksums, err := downloadString(repoDownload + "/checksums.txt")
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}

	expectedHash, err := parseChecksum(checksums, asset)
	if err != nil {
		return err
	}

	// Download binary to temp file next to current executable
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("eval symlinks: %w", err)
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "acopy-update-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpPath) // clean up on failure
	}()

	// Download binary
	resp, err := http.Get(repoDownload + "/" + asset)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download binary: %s", resp.Status)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		return fmt.Errorf("write binary: %w", err)
	}
	tmp.Close()

	// Verify checksum
	actualHash := hex.EncodeToString(h.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tmpPath, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}

// Run starts the background update checker. It checks immediately on start,
// then every checkInterval. If an update is found and applied, it calls
// os.Exit(ExitCodeUpdate) to trigger a service restart.
func Run(currentVersion string) {
	// Don't check if running a dev build
	if currentVersion == "dev" || currentVersion == "" {
		return
	}

	go func() {
		// Initial check after a short delay to let the daemon settle
		time.Sleep(30 * time.Second)
		checkAndUpdate(currentVersion)

		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for range ticker.C {
			checkAndUpdate(currentVersion)
		}
	}()
}

func checkAndUpdate(currentVersion string) {
	latest, err := CheckForUpdate(currentVersion)
	if err != nil {
		log.Printf("update check: %v", err)
		return
	}
	if latest == "" {
		return
	}

	log.Printf("update available: %s -> %s, downloading...", currentVersion, latest)

	if err := DownloadAndReplace(); err != nil {
		log.Printf("update failed: %v", err)
		return
	}

	log.Printf("updated to %s, restarting...", latest)
	restartAfterUpdate()
}

func downloadString(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("http %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseChecksum(checksums, filename string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		// Format: "hash  filename" (two spaces)
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == filename {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s", filename)
}

// RunOnce performs a single update check and prints the result.
// Returns true if an update was applied.
func RunOnce(currentVersion string) bool {
	if currentVersion == "dev" || currentVersion == "" {
		fmt.Println("skipping update check (dev build)")
		return false
	}

	fmt.Printf("checking for updates (current: %s)...\n", currentVersion)

	latest, err := CheckForUpdate(currentVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
		return false
	}
	if latest == "" {
		fmt.Println("already up to date")
		return false
	}

	fmt.Printf("downloading %s...\n", latest)
	if err := DownloadAndReplace(); err != nil {
		fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
		return false
	}

	fmt.Printf("updated to %s\n", latest)
	return true
}
