# acopy client - Design Document

A background daemon that syncs clipboard content across machines via the acopy server.

## How It Works

1. **Monitor** - Watch the local clipboard for changes (polling every 500ms)
2. **Push** - When a copy is detected, send the content to the server
3. **Pull** - Receive clipboard updates from other clients and overwrite the local clipboard
4. **Ignore echo** - Skip clipboard writes that originated from the pull, to avoid infinite loops

## Architecture

```
Machine A                      Server                      Machine B
---------                      ------                      ---------
[clipboard] --copy--> WS (binary frame) ---broadcast--> [clipboard]
[clipboard] <--paste-- <---broadcast--- WS (binary frame) <--copy-- [clipboard]
```

All clipboard sync happens over a single persistent WebSocket connection per client using binary frames. Auth endpoints (`/api/users/*`) remain JSON over HTTP.

### Wire Protocol

Every WebSocket message is a binary frame with this layout:

```
+--------+--------+--------+----------+---------+
| ver    | type   | flags  | length   | payload |
| 1 byte | 1 byte | 1 byte | 4 bytes  | N bytes |
+--------+--------+--------+----------+---------+
                            (big-endian)
```

**Header (7 bytes)**

| Field  | Size | Description |
|--------|------|-------------|
| ver    | 1    | Protocol version. Currently `0x01` |
| type   | 1    | Message type (see below) |
| flags  | 1    | Bit 0: zstd compressed. Bits 1-7: reserved |
| length | 4    | Payload length in bytes (big-endian uint32) |

**Message Types**

| Type | Hex  | Direction       | Description |
|------|------|-----------------|-------------|
| Auth | 0x01 | client -> server | Authenticate with JWT |
| ClipboardPush | 0x02 | client -> server | New clipboard content |
| ClipboardBroadcast | 0x03 | server -> client | Clipboard update from another device |
| Ack  | 0x04 | server -> client | Confirms a received message |
| Error | 0x05 | server -> client | Error with code and message |
| Ping | 0x06 | either | Keepalive |
| Pong | 0x07 | either | Keepalive response |

### Payload Encoding: MessagePack

Payloads are [MessagePack](https://msgpack.org)-encoded maps. MessagePack is a compact binary serialization format (~30-50% smaller than JSON for typical clipboard payloads).

**Auth (0x01)**
```
{
  "token": "eyJhbG..."
}
```

**ClipboardPush (0x02)**
```
{
  "content": <bytes>,
  "device": "macbook-pro"
}
```

**ClipboardBroadcast (0x03)**
```
{
  "content": <bytes>,
  "device": "desktop-pc",
  "ts": 1743465600
}
```

**Ack (0x04)**
```
{
  "ok": true
}
```

**Error (0x05)**
```
{
  "code": 401,
  "msg": "token expired"
}
```

### Compression

When the `flags` bit 0 is set, the payload is zstd-compressed before being sent. The receiver decompresses before decoding MessagePack.

Compression policy:
- **Always compress** payloads > 1 KB
- **Never compress** payloads <= 256 bytes
- **Compress if ratio > 10%** for payloads 256 bytes - 1 KB

This keeps small copies (short text, URLs) fast with zero overhead, while large copies (code blocks, logs) get significant size reduction.

### Connection Lifecycle

```
Client                              Server
  |                                    |
  |--- WS connect /ws ----------------->|
  |--- Auth (0x01, jwt) --------------->|
  |<-- Ack (0x04) ---------------------|  (or Error 0x05 if bad token)
  |                                    |
  |--- ClipboardPush (0x02) ----------->|  (user copies something)
  |<-- Ack (0x04) ---------------------|
  |              server broadcasts to other connections
  |                                    |
  |<-- ClipboardBroadcast (0x03) ------|  (another device copied)
  |                                    |
  |--- Ping (0x06) ------------------->|  (every 30s)
  |<-- Pong (0x07) --------------------|
  |                                    |
```

### Reconnection

On disconnect, the client reconnects with exponential backoff: 1s, 2s, 4s, 8s, ... capped at 30s. Resets to 1s after a successful connection that lasts > 60s.

## Server Changes Required

The server needs these additions:

### New Table: `clipboard_entries`

```sql
CREATE TABLE clipboard_entries (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  content BLOB NOT NULL,
  device_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);
```

### New Endpoints

| Method | Endpoint | Auth | Purpose |
|--------|----------|------|---------|
| WS | `/ws` | Via Auth message (0x01) | Binary protocol for clipboard sync |

Auth endpoints (`/api/users/*`) remain unchanged (JSON over HTTP).

### WebSocket Behavior

- Server maintains a map of `user_id -> [connections]`
- On Auth message, verify JWT and associate connection with user
- On ClipboardPush, store entry, broadcast ClipboardBroadcast to all other connections for that user
- The pushing connection is excluded from the broadcast (no echo)
- Server handles zstd: decompress on receive if flag set, re-compress on broadcast if payload is large

## Client Components

```
acopy (binary)
  |
  |- auth         Login / token storage
  |- clipboard    Platform-specific clipboard read/write
  |- monitor      Polls clipboard, detects changes
  |- protocol     Binary message encode/decode + zstd
  |- sync         WebSocket connection management
  |- config       Config file (~/.config/acopy/config.toml)
  \- daemon       Background process management
```

### Auth

- `acopy login` - prompts for email/password, calls `POST /api/users/login`, stores JWT in `~/.config/acopy/config.toml`
- `acopy register` - prompts for email/password, calls `POST /api/users/register`
- Token refresh: re-login when token expires (server returns 401)

### Clipboard Access

Platform-specific:

| Platform | Read | Write |
|----------|------|-------|
| macOS | `pbpaste` | `pbcopy` |
| Linux | `xclip -selection clipboard -o` | `xclip -selection clipboard` |
| Windows | `powershell Get-Clipboard` | `powershell Set-Clipboard` |

### Monitor Loop

Only hashes the clipboard content, never holds full content in memory until a change is detected:

```
last_hash = hash(read_clipboard())
last_was_remote = false

loop:
  sleep(500ms)
  current_hash = clipboard_change_count()  // macOS: NSPasteboard.changeCount (no data read)

  if current_hash != last_hash:
    last_hash = current_hash
    if last_was_remote:
      last_was_remote = false  // skip, this was our own write
      continue
    content = read_clipboard()
    push_to_server(content)
```

On macOS, `NSPasteboard.changeCount` is a simple integer check with no memory allocation. The actual clipboard content is only read when a real change is detected.

### Sync (WebSocket)

```
on_message(msg):
  write_clipboard(msg.content)
  last_was_remote = true
  last_hash = clipboard_change_count()
```

### Echo Prevention

The clipboard monitor and WebSocket receiver share state:
- When a remote update writes to the clipboard, set a flag
- The monitor sees the change but skips pushing because the flag is set
- The flag is cleared after one cycle

## Performance

Target: **< 5 MB RSS, < 0.1% CPU** when idle.

### CPU

- **No-read polling**: On macOS, check `NSPasteboard.changeCount` (an integer) instead of reading clipboard contents. This avoids copying potentially large clipboard data every 500ms. Linux/Windows: compare a lightweight clipboard sequence number or hash.
- **Single goroutine for monitor**: One ticker, one goroutine. No extra threads.
- **WebSocket is idle when no activity**: Gorilla WebSocket blocks on `ReadMessage()` with no CPU cost. Only the 30s keepalive ping wakes it.
- **No busy loops**: All waits use `time.Ticker` or channel receives.

### Memory

- **Stream, don't buffer**: Clipboard content is read once into a `[]byte`, encoded directly into a WebSocket frame, then freed. No intermediate copies.
- **Zstd dictionary reuse**: Keep a single `zstd.Encoder` and `zstd.Decoder` alive with pre-allocated buffers. Don't create per-message.
- **Cap clipboard size**: Reject clipboard content > 10 MB. Anything larger is likely not intentional cross-device sync.
- **No clipboard history**: Only the latest entry exists in memory. Previous entries are GC'd immediately.

### Network

- **Compression**: Zstd on payloads > 1 KB reduces bandwidth significantly for code/logs (typical 3-5x reduction).
- **Binary protocol overhead**: 6-byte header + msgpack encoding. A typical short text copy is ~50-100 bytes on the wire vs ~200+ bytes for equivalent JSON+HTTP.
- **Keepalive**: 30s ping interval. Minimal bandwidth (6 bytes per ping/pong).

## Config File

`~/.config/acopy/config.toml`

```toml
server_url = "https://acopy.example.com"
device_name = "macbook-pro"
token = "eyJhbG..."
```

## CLI Commands

```
acopy register          Register a new account
acopy login             Login and store token
acopy start             Start the daemon (foreground)
acopy start -d          Start the daemon (background)
acopy stop              Stop the background daemon
acopy status            Show connection status
acopy paste             Print the latest remote clipboard
```

## Tech Stack

**Go** - single binary, cross-platform, good WebSocket libraries, easy clipboard access via exec.

Dependencies:
- `gorilla/websocket` - WebSocket client
- `vmihailenco/msgpack/v5` - MessagePack encode/decode
- `klauspost/compress/zstd` - Zstd compression
- `pelletier/go-toml` - config parsing
- Standard library for everything else (HTTP, crypto, exec, os/signal)

On macOS, uses cgo to call `NSPasteboard.changeCount` for zero-copy clipboard change detection.

## Scope Constraints

- **Text only** - no images or files in v1
- **No history** - only the latest clipboard entry matters
- **No encryption** - relies on HTTPS/WSS transport security (e2e encryption is a future consideration)
- **Single user** - syncs across devices for one account, no sharing between users
