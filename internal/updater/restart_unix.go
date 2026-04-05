//go:build !windows

package updater

import "os"

// restartAfterUpdate exits with ExitCodeUpdate so the service manager
// (systemd/launchd) restarts the process with the new binary.
func restartAfterUpdate() {
	os.Exit(ExitCodeUpdate)
}
