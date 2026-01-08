# WebSockets

SyftBox uses WebSockets for low-latency, bidirectional messaging between clients and the server. WebSockets power:

- Real-time sync notifications and priority file transfers (files `<= 4MB`)
- Online RPC delivery (Send Handler → WebSocket dispatcher)
- System and error events

This document describes the WebSocket endpoint, message types, and the on-wire encoding/negotiation used for backward-compatible upgrades.

## Overview

**Endpoint:** `GET /api/v1/events`  
**Auth:** same headers/cookies as the HTTP API (e.g. `Authorization: Bearer …`)  
**Client library:** `internal/syftsdk/events.go`  
**Server handler:** `internal/server/handlers/ws/ws_hub.go`

Once connected, the client and server exchange `syftmsg.Message` objects. Messages are typed and routed by `Message.Type` (see `internal/syftmsg/msg_type.go`).

## Message Model

At the application level, every frame carries a `syftmsg.Message`:

```go
type Message struct {
    Id   string      `json:"id"`
    Type MessageType `json:"typ"`
    Data any         `json:"dat"`
}
```

`Data` is a concrete struct depending on `Type`, for example:

- `MsgFileWrite` → `syftmsg.FileWrite` (`pth`, `etg`, `len`, `con`)
- `MsgAck`/`MsgNack` → `syftmsg.Ack`/`syftmsg.Nack`
- `MsgSystem` → `syftmsg.System`

## Encoding and Negotiation

Historically, SyftBox sent messages as **JSON text frames**. In JSON, `[]byte` fields are base64-encoded, adding ~33% size overhead and CPU cost for encode/decode.

WebSockets natively support binary frames, so SyftBox now supports a binary encoding that avoids base64 while remaining backward compatible.

### Supported Encodings

| Encoding        | WebSocket frame | Notes                                                     |
| --------------- | --------------- | --------------------------------------------------------- |
| `json` (legacy) | Text            | `[]byte` fields are base64 (Go `encoding/json` behavior). |
| `msgpack` (v1)  | Binary          | Native `[]byte`, no base64. Uses a small envelope.        |

### Client Capability Header

New clients advertise supported encodings in the upgrade request:

```
X-Syft-WS-Encodings: msgpack,json
```

Order matters: the first supported encoding is preferred.

Old clients do not send this header.

### Server Selection Header

During the WebSocket upgrade, the server selects an encoding and returns:

```
X-Syft-WS-Encoding: msgpack
```

If the header is missing, clients must assume `json`.

### Binary Envelope

Binary frames use:

```
[ 2 bytes magic ][ 1 byte version ][ 1 byte encoding ][ payload... ]

magic    = 'S','B'
version  = 1
encoding = 0x01 (msgpack)
payload  = MessagePack-encoded syftmsg.Message
```

The envelope allows future protocol upgrades (new versions or encodings) without breaking older deployments.

### Backward Compatibility

- **New server + old client:**  
  Old client sends text JSON → server decodes as legacy JSON.  
  Old client doesn’t advertise encodings → server defaults to `json` and replies with text JSON.

- **Old server + new client:**  
  Old server ignores `X-Syft-WS-Encodings` and replies without `X-Syft-WS-Encoding`.  
  New client falls back to `json` and uses legacy text frames.

No coordinated upgrade is required; either side can be upgraded independently.

## Message Size Limits

Both client and server currently enforce an **8MB read limit** per WebSocket message:

- Client: `wsClientMaxMessageSize = 8MB` (`internal/syftsdk/events.go`)
- Server: `maxMessageSize = 8MB` (`internal/server/handlers/ws/ws_hub.go`)

This limit was increased from 4MB to accommodate the legacy JSON+base64 overhead for priority file writes. With `msgpack`, raw 4MB file payloads fit comfortably within the limit.

## Priority File Writes

Priority sync sends small files directly over WebSockets:

1. Client detects a priority file change (`*.request`, ACL files, etc.).
2. Client constructs `MsgFileWrite` with raw content:
   - Path (`pth`)
   - ETag (`etg`)
   - Length (`len`)
   - Content (`con`)
3. Client sends over WebSocket and waits for `Ack`/`Nack`.
4. Server validates ACL permissions, writes to blob storage, and broadcasts to peers.

Priority uploads are limited to files `<= 4MB` (see `internal/client/sync/sync_engine_priority_upload.go`).

## Error Handling

- Decode failures are logged and the frame is dropped.
- Connection closures:
  - Normal closure (`1000`) stops loops cleanly.
  - Abnormal closures are logged for diagnosis.

`Ack`/`Nack` are used for reliability in priority uploads.

## Extending the Protocol

To introduce a new encoding or change message structure:

1. Add a new `Encoding` constant in `internal/wsproto/codec.go`.
2. Teach server/client to advertise and negotiate it.
3. Bump envelope `version` if the wire layout changes.
4. Keep decoding of older versions/encodings for compatibility.
