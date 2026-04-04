package org.acopy.android

import com.github.luben.zstd.Zstd
import org.msgpack.core.MessagePack
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer

object Protocol {
    const val VERSION: Byte = 0x01
    const val HEADER_SIZE = 7
    const val FLAG_COMPRESSED: Byte = 0x01
    const val COMPRESS_THRESHOLD = 1024
    const val MAX_PAYLOAD_SIZE = 10 * 1024 * 1024
}

enum class MsgType(val value: Byte) {
    AUTH(0x01),
    CLIPBOARD_PUSH(0x02),
    CLIPBOARD_BROADCAST(0x03),
    ACK(0x04),
    ERROR(0x05),
    PING(0x06),
    PONG(0x07),
    COPY_INTENT(0x08),
    COPY_CANCEL(0x09),
    DEVICE_RENAMED(0x0A),
    DEVICE_DELETED(0x0B);

    companion object {
        fun from(value: Byte): MsgType? = entries.find { it.value == value }
    }
}

data class AuthPayload(val token: String)
data class ClipboardPushPayload(val content: ByteArray, val device: String, val contentType: String = "text/plain")
data class ClipboardBroadcastPayload(val id: String = "", val content: ByteArray, val device: String, val contentType: String = "text/plain", val ts: Long = 0)
data class ErrorPayload(val code: Int, val msg: String)
data class AckPayload(val deviceId: String? = null)
data class DeviceRenamedPayload(val deviceId: String, val oldName: String, val newName: String)
data class DeviceDeletedPayload(val deviceId: String)

object Codec {

    fun encode(msgType: MsgType, payload: ByteArray?): ByteArray {
        var body = payload ?: ByteArray(0)
        var flags: Byte = 0

        if (body.size > Protocol.COMPRESS_THRESHOLD) {
            val compressed = Zstd.compress(body, 1)
            if (compressed.size < body.size) {
                body = compressed
                flags = Protocol.FLAG_COMPRESSED
            }
        }

        val frame = ByteBuffer.allocate(Protocol.HEADER_SIZE + body.size)
        frame.put(Protocol.VERSION)
        frame.put(msgType.value)
        frame.put(flags)
        frame.putInt(body.size)
        frame.put(body)
        return frame.array()
    }

    fun decode(frame: ByteArray): Pair<MsgType, ByteArray> {
        require(frame.size >= Protocol.HEADER_SIZE) { "frame too short" }
        require(frame[0] == Protocol.VERSION) { "unknown protocol version: ${frame[0]}" }

        val msgType = MsgType.from(frame[1])
            ?: throw IllegalArgumentException("unknown message type: ${frame[1]}")
        val flags = frame[2]
        val length = ByteBuffer.wrap(frame, 3, 4).int

        require(length <= Protocol.MAX_PAYLOAD_SIZE) { "payload too large: $length bytes" }
        require(frame.size >= Protocol.HEADER_SIZE + length) { "frame truncated" }

        var body = frame.copyOfRange(Protocol.HEADER_SIZE, Protocol.HEADER_SIZE + length)

        if (flags.toInt() and Protocol.FLAG_COMPRESSED.toInt() != 0) {
            @Suppress("DEPRECATION")
            val origSize = Zstd.decompressedSize(body)
            body = Zstd.decompress(body, origSize.toInt())
        }

        return msgType to body
    }

    // --- msgpack encoding helpers ---

    fun encodeAuth(token: String): ByteArray {
        val out = ByteArrayOutputStream()
        MessagePack.newDefaultPacker(out).use { packer ->
            packer.packMapHeader(1)
            packer.packString("token")
            packer.packString(token)
        }
        return out.toByteArray()
    }

    fun encodeClipboardPush(content: ByteArray, device: String, contentType: String = "text/plain"): ByteArray {
        val out = ByteArrayOutputStream()
        MessagePack.newDefaultPacker(out).use { packer ->
            packer.packMapHeader(3)
            packer.packString("content")
            packer.packBinaryHeader(content.size)
            packer.writePayload(content)
            packer.packString("device")
            packer.packString(device)
            packer.packString("content_type")
            packer.packString(contentType)
        }
        return out.toByteArray()
    }

    fun encodeCopyIntent(device: String): ByteArray {
        val out = ByteArrayOutputStream()
        MessagePack.newDefaultPacker(out).use { packer ->
            packer.packMapHeader(1)
            packer.packString("device")
            packer.packString(device)
        }
        return out.toByteArray()
    }

    fun encodePing(): ByteArray? = null

    // --- msgpack decoding helpers ---

    fun decodeAck(raw: ByteArray): AckPayload {
        var deviceId: String? = null
        if (raw.isEmpty()) return AckPayload()
        MessagePack.newDefaultUnpacker(raw).use { unpacker ->
            val mapSize = unpacker.unpackMapHeader()
            repeat(mapSize) {
                when (unpacker.unpackString()) {
                    "device_id" -> deviceId = unpacker.unpackString()
                    else -> unpacker.skipValue()
                }
            }
        }
        return AckPayload(deviceId)
    }

    fun decodeClipboardBroadcast(raw: ByteArray): ClipboardBroadcastPayload {
        var id = ""
        var content = ByteArray(0)
        var device = ""
        var contentType = "text/plain"
        var ts = 0L
        MessagePack.newDefaultUnpacker(raw).use { unpacker ->
            val mapSize = unpacker.unpackMapHeader()
            repeat(mapSize) {
                when (unpacker.unpackString()) {
                    "id" -> id = unpacker.unpackString()
                    "content" -> {
                        val len = unpacker.unpackBinaryHeader()
                        content = unpacker.readPayload(len)
                    }
                    "device" -> device = unpacker.unpackString()
                    "content_type" -> contentType = unpacker.unpackString()
                    "ts" -> ts = unpacker.unpackLong()
                    else -> unpacker.skipValue()
                }
            }
        }
        return ClipboardBroadcastPayload(id, content, device, contentType, ts)
    }

    fun decodeError(raw: ByteArray): ErrorPayload {
        var code = 0
        var msg = ""
        MessagePack.newDefaultUnpacker(raw).use { unpacker ->
            val mapSize = unpacker.unpackMapHeader()
            repeat(mapSize) {
                when (unpacker.unpackString()) {
                    "code" -> code = unpacker.unpackInt()
                    "msg" -> msg = unpacker.unpackString()
                    else -> unpacker.skipValue()
                }
            }
        }
        return ErrorPayload(code, msg)
    }

    fun decodeDeviceRenamed(raw: ByteArray): DeviceRenamedPayload {
        var deviceId = ""
        var oldName = ""
        var newName = ""
        MessagePack.newDefaultUnpacker(raw).use { unpacker ->
            val mapSize = unpacker.unpackMapHeader()
            repeat(mapSize) {
                when (unpacker.unpackString()) {
                    "device_id" -> deviceId = unpacker.unpackString()
                    "old_name" -> oldName = unpacker.unpackString()
                    "new_name" -> newName = unpacker.unpackString()
                    else -> unpacker.skipValue()
                }
            }
        }
        return DeviceRenamedPayload(deviceId, oldName, newName)
    }

    fun decodeDeviceDeleted(raw: ByteArray): DeviceDeletedPayload {
        var deviceId = ""
        MessagePack.newDefaultUnpacker(raw).use { unpacker ->
            val mapSize = unpacker.unpackMapHeader()
            repeat(mapSize) {
                when (unpacker.unpackString()) {
                    "device_id" -> deviceId = unpacker.unpackString()
                    else -> unpacker.skipValue()
                }
            }
        }
        return DeviceDeletedPayload(deviceId)
    }
}
