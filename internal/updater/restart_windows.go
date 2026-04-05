//go:build windows

package updater

import (
	"log"
	"os"
	"os/exec"
)

// restartAfterUpdate spawns the new binary via Task Scheduler and exits,
// since Windows Task Scheduler doesn't auto-restart on exit.
func restartAfterUpdate() {
	if err := exec.Command("schtasks", "/Run", "/TN", "acopy").Start(); err != nil {
		log.Printf("restart via schtasks: %v", err)
	}
	os.Exit(0)
}
