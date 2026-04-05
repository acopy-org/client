# acopy

Shared clipboard across machines.

## Install

**macOS / Linux:**

```bash
curl -fsSL https://acopy.org/install.sh | sh
```

The binary will be installed to:
- macOS: `/usr/local/bin/acopy`
- Linux: `~/.local/bin/acopy` (ensure `~/.local/bin` is in your PATH)

**Windows (PowerShell):**

```powershell
irm https://acopy.org/install.ps1 | iex
```

The binary will be installed to `%LOCALAPPDATA%\acopy\`

**After installation:**

1. Close and reopen your terminal (or refresh your PATH)
2. Verify installation: `acopy --version`
3. Run setup to register and install as a system service:

```bash
acopy setup
```

## Uninstall

```
acopy remove
```

This stops the service and removes it from startup. To also delete the binary:

| OS | Command |
|---|---|
| macOS | `sudo rm /usr/local/bin/acopy` |
| Linux | `rm ~/.local/bin/acopy` |
| Windows | `Remove-Item "$env:LOCALAPPDATA\acopy" -Recurse` |

Config is stored at `~/.config/acopy/` — delete it to remove all settings and tokens.

## Commands

```
acopy setup     # register/login + install as system service
acopy status    # show config and service status
acopy remove    # stop and remove system service
```

## How It Works

The client runs as a background service. It monitors the local clipboard for changes and syncs them to all other devices signed into the same account via a persistent WebSocket connection to acopy.org.

**Clipboard monitoring** uses native OS APIs to detect changes without reading clipboard content on every poll cycle. On macOS, it checks NSPasteboard's change count via cgo. On Linux, it subscribes to X11 XFixes selection change events via cgo. On Windows, it calls GetClipboardSequenceNumber via syscall. All three are simple integer checks with no memory allocation. The actual clipboard content is only read when a change is detected.

**Wire protocol** uses a compact binary format over WebSocket. Each message has a 7-byte header (version, type, flags, payload length) followed by a MessagePack-encoded payload. Payloads over 1 KB are compressed with zstd. A typical short text copy is ~50-100 bytes on the wire.

**Echo prevention** ensures that receiving a remote clipboard update doesn't trigger a push back to the server. When a remote update writes to the local clipboard, a flag is set. The monitor sees the change but skips pushing because the flag is set. The flag clears after one cycle.

**Offline handling** queues the latest clipboard push when disconnected. On reconnect, the queued content is flushed to the server. Only the most recent copy is kept — older ones are stale.

**Reconnection** uses exponential backoff starting at 1 second, doubling up to 30 seconds. The backoff resets after a stable connection lasting more than 60 seconds.

**Keepalive** pings are sent every 30 seconds to detect dead connections early.

## Resource Usage

Target: < 5 MB RSS, < 0.1% CPU when idle.

Three goroutines run at steady state: the clipboard monitor (500ms ticker), the WebSocket reader (blocked on I/O), and a ping ticker (30s). The zstd encoder and decoder are allocated once and reused. Clipboard content over 10 MB is rejected.

## Prerequisites

**Linux:** `xclip` must be installed and a display server (X11) must be available. The systemd service sets `DISPLAY=:0` by default.

```
sudo apt install xclip      # Debian/Ubuntu
sudo pacman -S xclip         # Arch
sudo dnf install xclip       # Fedora
```

## Platform Details

| | macOS | Linux | Windows |
|---|---|---|---|
| Change detection | NSPasteboard.changeCount (osascript) | xclip TARGETS hash | GetClipboardSequenceNumber (syscall) |
| Clipboard read | pbpaste / osascript (images) | xclip | Get-Clipboard |
| Clipboard write | pbcopy / osascript (images) | xclip | clip.exe |
| Service | launchd | systemd (user) | schtasks (logon) |
| Binary location | /usr/local/bin | ~/.local/bin | %LOCALAPPDATA%\acopy |

## Build from source

**Prerequisites:** Go 1.21 or later

```bash
# Clone the repository
git clone https://github.com/your-org/acopy.git
cd acopy/client

# Build the binary
go build -o acopy ./cmd/acopy

# (Optional) Install to your PATH
# macOS/Linux:
sudo cp acopy /usr/local/bin/  # macOS
# or
cp acopy ~/.local/bin/         # Linux

# Windows:
# Copy acopy.exe to a directory in your PATH
```

After building, run `acopy setup` to register and install as a system service.

## License

MIT
