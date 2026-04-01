package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	Version    = 0x01
	HeaderSize = 7

	FlagCompressed = 0x01

	compressThreshold = 1024

	MaxPayloadSize = 10 * 1024 * 1024 // 10 MB
)

type MsgType byte

const (
	MsgAuth               MsgType = 0x01
	MsgClipboardPush      MsgType = 0x02
	MsgClipboardBroadcast MsgType = 0x03
	MsgAck                MsgType = 0x04
	MsgError              MsgType = 0x05
	MsgPing               MsgType = 0x06
	MsgPong               MsgType = 0x07
)

// Payloads

type AuthPayload struct {
	Token string `msgpack:"token"`
}

type ClipboardPushPayload struct {
	Content     []byte `msgpack:"content"`
	Device      string `msgpack:"device"`
	ContentType string `msgpack:"content_type"`
}

type ClipboardBroadcastPayload struct {
	ID          string `msgpack:"id"`
	Content     []byte `msgpack:"content"`
	Device      string `msgpack:"device"`
	ContentType string `msgpack:"content_type"`
	TS          int64  `msgpack:"ts"`
}

type AckPayload struct {
	OK bool `msgpack:"ok"`
}

type ErrorPayload struct {
	Code int    `msgpack:"code"`
	Msg  string `msgpack:"msg"`
}

// Codec holds reusable zstd encoder/decoder.
type Codec struct {
	enc *zstd.Encoder
	dec *zstd.Decoder
}

func NewCodec() (*Codec, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, fmt.Errorf("zstd encoder: %w", err)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		enc.Close()
		return nil, fmt.Errorf("zstd decoder: %w", err)
	}
	return &Codec{enc: enc, dec: dec}, nil
}

func (c *Codec) Close() {
	c.enc.Close()
	c.dec.Close()
}

// Encode serializes a message into a binary frame.
func (c *Codec) Encode(msgType MsgType, payload any) ([]byte, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = msgpack.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("msgpack encode: %w", err)
		}
	}

	var flags byte
	if len(body) > 0 {
		body, flags = c.maybeCompress(body)
	}

	frame := make([]byte, HeaderSize+len(body))
	frame[0] = Version
	frame[1] = byte(msgType)
	frame[2] = flags
	binary.BigEndian.PutUint32(frame[3:7], uint32(len(body)))
	copy(frame[HeaderSize:], body)
	return frame, nil
}

// Decode parses a binary frame into a message type and raw payload bytes (decompressed, msgpack-encoded).
func (c *Codec) Decode(frame []byte) (MsgType, []byte, error) {
	if len(frame) < HeaderSize {
		return 0, nil, errors.New("frame too short")
	}
	if frame[0] != Version {
		return 0, nil, fmt.Errorf("unknown protocol version: %d", frame[0])
	}

	msgType := MsgType(frame[1])
	flags := frame[2]
	length := binary.BigEndian.Uint32(frame[3:7])

	if length > MaxPayloadSize {
		return 0, nil, fmt.Errorf("payload too large: %d bytes", length)
	}
	if len(frame) < HeaderSize+int(length) {
		return 0, nil, fmt.Errorf("frame truncated: want %d, got %d", HeaderSize+int(length), len(frame))
	}

	body := frame[HeaderSize : HeaderSize+int(length)]

	if flags&FlagCompressed != 0 {
		decompressed, err := c.dec.DecodeAll(body, nil)
		if err != nil {
			return 0, nil, fmt.Errorf("zstd decompress: %w", err)
		}
		body = decompressed
	}

	return msgType, body, nil
}

// DecodePayload unmarshals raw msgpack bytes into the target struct.
func DecodePayload[T any](raw []byte) (*T, error) {
	var v T
	if len(raw) == 0 {
		return &v, nil
	}
	if err := msgpack.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("msgpack decode: %w", err)
	}
	return &v, nil
}

func (c *Codec) maybeCompress(data []byte) ([]byte, byte) {
	if len(data) <= compressThreshold {
		return data, 0
	}
	compressed := c.enc.EncodeAll(data, make([]byte, 0, len(data)))
	if len(compressed) < len(data) {
		return compressed, FlagCompressed
	}
	return data, 0
}
