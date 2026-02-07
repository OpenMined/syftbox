use anyhow::Result;
use base64::Engine;
use serde::de::{SeqAccess, Visitor};
use serde::{Deserialize, Serialize};
use std::fmt;

pub const WS_MAX_MESSAGE_BYTES: usize = 8 * 1024 * 1024;

const MAGIC0: u8 = b'S';
const MAGIC1: u8 = b'B';
const VERSION: u8 = 1;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Encoding {
    Json,
    MsgPack,
}

impl Encoding {
    pub fn as_str(self) -> &'static str {
        match self {
            Encoding::Json => "json",
            Encoding::MsgPack => "msgpack",
        }
    }

    pub fn as_byte(self) -> u8 {
        match self {
            Encoding::Json => 0,
            Encoding::MsgPack => 1,
        }
    }
}

pub fn preferred_encoding(header: &str) -> Encoding {
    match header.trim().to_lowercase().as_str() {
        "msgpack" => Encoding::MsgPack,
        "json" => Encoding::Json,
        _ => Encoding::Json,
    }
}

#[derive(Debug, Deserialize)]
pub struct Message {
    pub id: String,
    #[serde(rename = "typ")]
    pub typ: u16,
    #[serde(rename = "dat")]
    pub dat: serde_json::Value,
}

#[derive(Debug, Clone)]
pub struct FileWrite {
    pub path: String,
    pub etag: String,
    pub length: i64,
    pub content: Option<Vec<u8>>,
}

#[derive(Debug, Deserialize)]
struct JsonFileWrite {
    #[serde(rename = "pth")]
    pub path: String,
    #[serde(rename = "etg")]
    pub etag: String,
    #[serde(rename = "len")]
    pub length: i64,
    #[serde(rename = "con", default, deserialize_with = "deserialize_base64_opt")]
    pub content: Option<Vec<u8>>,
}

// Go msgpack encoding uses exported field names, not `json:"pth"` tags.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgpackFileWrite {
    #[serde(rename = "Path")]
    pub path: String,
    #[serde(rename = "ETag")]
    pub etag: String,
    #[serde(rename = "Length")]
    pub length: i64,
    #[serde(rename = "Content", default)]
    pub content: Option<MpBytes>,
}

#[derive(Debug, Clone)]
pub struct HotlinkOpen {
    pub session_id: String,
    pub path: String,
}

#[derive(Debug, Clone)]
pub struct HotlinkAccept {
    pub session_id: String,
}

#[derive(Debug, Clone)]
pub struct HotlinkReject {
    pub session_id: String,
    pub reason: String,
}

#[derive(Debug, Clone)]
pub struct HotlinkData {
    pub session_id: String,
    pub seq: u64,
    pub path: String,
    pub etag: String,
    pub payload: Option<Vec<u8>>,
}

#[derive(Debug, Clone)]
pub struct HotlinkClose {
    pub session_id: String,
    #[allow(dead_code)]
    pub reason: String,
}

#[derive(Debug, Clone)]
pub struct HotlinkSignal {
    pub session_id: String,
    pub kind: String,
    pub addrs: Vec<String>,
    pub token: String,
    pub error: String,
}

#[derive(Debug, Deserialize)]
struct JsonHotlinkOpen {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "pth")]
    pub path: String,
}

#[derive(Debug, Deserialize)]
struct JsonHotlinkAccept {
    #[serde(rename = "sid")]
    pub session_id: String,
}

#[derive(Debug, Deserialize)]
struct JsonHotlinkReject {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "rsn", default)]
    pub reason: String,
}

#[derive(Debug, Deserialize)]
struct JsonHotlinkData {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "seq")]
    pub seq: u64,
    #[serde(rename = "pth")]
    pub path: String,
    #[serde(rename = "etg", default)]
    pub etag: String,
    #[serde(rename = "pay", default, deserialize_with = "deserialize_base64_opt")]
    pub payload: Option<Vec<u8>>,
}

#[derive(Debug, Deserialize)]
struct JsonHotlinkClose {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "rsn", default)]
    pub reason: String,
}

#[derive(Debug, Deserialize)]
struct JsonHotlinkSignal {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "knd")]
    pub kind: String,
    #[serde(rename = "adr", default)]
    pub addrs: Vec<String>,
    #[serde(rename = "tok", default)]
    pub token: String,
    #[serde(rename = "err", default)]
    pub error: String,
}

// Hotlink msgpack uses msgpack tags ("sid", "pth", etc) from Go.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgpackHotlinkOpen {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "pth")]
    pub path: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgpackHotlinkAccept {
    #[serde(rename = "sid")]
    pub session_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgpackHotlinkReject {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "rsn", default)]
    pub reason: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgpackHotlinkData {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "seq")]
    pub seq: u64,
    #[serde(rename = "pth")]
    pub path: String,
    #[serde(rename = "etg", default)]
    pub etag: String,
    #[serde(rename = "pay", default)]
    pub payload: Option<MpBytes>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgpackHotlinkClose {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "rsn", default)]
    pub reason: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct MsgpackHotlinkSignal {
    #[serde(rename = "sid")]
    pub session_id: String,
    #[serde(rename = "knd")]
    pub kind: String,
    #[serde(rename = "adr", default)]
    pub addrs: Vec<String>,
    #[serde(rename = "tok", default)]
    pub token: String,
    #[serde(rename = "err", default)]
    pub error: String,
}

#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct ACLEntry {
    pub path: String,
    pub hash: String,
}

#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct ACLManifest {
    pub version: i32,
    pub datasite: String,
    pub for_user: String,
    pub for_hash: String,
    pub generated: String,
    pub acl_order: Vec<ACLEntry>,
}

#[derive(Debug, Deserialize)]
struct JsonACLEntry {
    pub path: String,
    pub hash: String,
}

#[derive(Debug, Deserialize)]
struct JsonACLManifest {
    pub version: i32,
    pub datasite: String,
    #[serde(rename = "for")]
    pub for_user: String,
    pub for_hash: String,
    pub generated: String,
    pub acl_order: Vec<JsonACLEntry>,
}

#[derive(Debug, Deserialize)]
struct MsgpackACLEntry {
    #[serde(rename = "Path")]
    pub path: String,
    #[serde(rename = "Hash")]
    pub hash: String,
}

#[derive(Debug, Deserialize)]
struct MsgpackACLManifest {
    #[serde(rename = "Version")]
    pub version: i32,
    #[serde(rename = "Datasite")]
    pub datasite: String,
    #[serde(rename = "For")]
    pub for_user: String,
    #[serde(rename = "ForHash")]
    pub for_hash: String,
    #[serde(rename = "Generated")]
    pub generated: String,
    #[serde(rename = "ACLOrder")]
    pub acl_order: Vec<MsgpackACLEntry>,
}

#[derive(Debug, Clone)]
pub struct Ack {
    pub original_id: String,
}

#[derive(Debug, Clone)]
pub struct Nack {
    pub original_id: String,
    pub error: String,
}

#[derive(Debug, Deserialize)]
struct JsonAck {
    #[serde(rename = "oid")]
    pub original_id: String,
}

#[derive(Debug, Deserialize)]
struct JsonNack {
    #[serde(rename = "oid")]
    pub original_id: String,
    #[serde(rename = "err")]
    pub error: String,
}

#[derive(Debug, Deserialize)]
struct MsgpackAck {
    #[serde(rename = "OriginalId")]
    pub original_id: String,
}

#[derive(Debug, Deserialize)]
struct MsgpackNack {
    #[serde(rename = "OriginalId")]
    pub original_id: String,
    #[serde(rename = "Error")]
    pub error: String,
}

#[derive(Debug, Clone)]
pub struct HttpMsg {
    pub syft_url: String,
    pub id: String,
    pub body: Option<Vec<u8>>,
}

#[derive(Debug, Deserialize)]
struct JsonHttpMsg {
    #[serde(rename = "syft_url")]
    pub syft_url: String,
    pub id: String,
    #[serde(default, deserialize_with = "deserialize_base64_opt")]
    pub body: Option<Vec<u8>>,
}

// Go msgpack encoding uses exported field names and nested SyftURL struct.
#[derive(Debug, Deserialize)]
struct MsgpackSyftURL {
    #[serde(rename = "Datasite")]
    datasite: String,
    #[serde(rename = "AppName")]
    app_name: String,
    #[serde(rename = "Endpoint")]
    endpoint: String,
}

#[derive(Debug, Deserialize)]
struct MsgpackHttpMsg {
    #[serde(rename = "SyftURL")]
    syft_url: MsgpackSyftURL,
    #[serde(rename = "Id")]
    id: String,
    #[serde(rename = "Body", default)]
    body: Option<MpBytes>,
}

#[derive(Debug)]
pub enum Decoded {
    FileWrite(FileWrite),
    HotlinkOpen(HotlinkOpen),
    HotlinkAccept(HotlinkAccept),
    HotlinkReject(HotlinkReject),
    HotlinkData(HotlinkData),
    HotlinkClose(HotlinkClose),
    HotlinkSignal(HotlinkSignal),
    Http(HttpMsg),
    Ack(Ack),
    Nack(Nack),
    ACLManifest(ACLManifest),
    Other { id: String, typ: u16 },
}

/// MsgPack "bin" compatibility wrapper.
///
/// Go's msgpack implementation represents `[]byte` as msgpack `bin` and expects the same on decode.
/// `Vec<u8>` serializes as a sequence in serde by default, so we wrap it to force `serialize_bytes`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct MpBytes(pub(crate) Vec<u8>);

impl From<Vec<u8>> for MpBytes {
    fn from(value: Vec<u8>) -> Self {
        Self(value)
    }
}

impl Serialize for MpBytes {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        serializer.serialize_bytes(&self.0)
    }
}

impl<'de> Deserialize<'de> for MpBytes {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        struct MpBytesVisitor;

        impl<'de> Visitor<'de> for MpBytesVisitor {
            type Value = MpBytes;

            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                write!(f, "msgpack bin/bytes or sequence of u8")
            }

            fn visit_bytes<E>(self, v: &[u8]) -> Result<Self::Value, E>
            where
                E: serde::de::Error,
            {
                Ok(MpBytes(v.to_vec()))
            }

            fn visit_byte_buf<E>(self, v: Vec<u8>) -> Result<Self::Value, E>
            where
                E: serde::de::Error,
            {
                Ok(MpBytes(v))
            }

            fn visit_seq<A>(self, mut seq: A) -> Result<Self::Value, A::Error>
            where
                A: SeqAccess<'de>,
            {
                let mut out = Vec::new();
                while let Some(b) = seq.next_element::<u8>()? {
                    out.push(b);
                }
                Ok(MpBytes(out))
            }
        }

        deserializer.deserialize_any(MpBytesVisitor)
    }
}

#[derive(Debug, Deserialize, Serialize)]
struct WireMessage {
    pub id: String,
    #[serde(rename = "typ")]
    pub typ: u16,
    #[serde(rename = "dat")]
    pub dat: MpBytes,
}

pub fn encode_msgpack<T: Serialize>(id: &str, typ: u16, dat: &T) -> Result<Vec<u8>> {
    let dat_bytes = rmp_serde::to_vec_named(dat)?;
    let wire = WireMessage {
        id: id.to_string(),
        typ,
        dat: MpBytes(dat_bytes),
    };
    let payload = rmp_serde::to_vec_named(&wire)?;

    let mut out = Vec::with_capacity(4 + payload.len());
    out.push(MAGIC0);
    out.push(MAGIC1);
    out.push(VERSION);
    out.push(Encoding::MsgPack.as_byte());
    out.extend_from_slice(&payload);
    Ok(out)
}

pub fn decode_text_json(raw: &str) -> Result<Decoded> {
    let msg: Message = serde_json::from_str(raw)?;
    decode_json_msg(msg)
}

pub fn decode_binary(raw: &[u8]) -> Result<Decoded> {
    if raw.len() >= 4 && raw[0] == MAGIC0 && raw[1] == MAGIC1 {
        if raw[2] != VERSION {
            anyhow::bail!("unsupported ws envelope version: {}", raw[2]);
        }
        let enc = raw[3];
        let payload = &raw[4..];
        match enc {
            1 => decode_msgpack(payload),
            0 => {
                // Allow binary JSON envelopes if ever used.
                let txt = std::str::from_utf8(payload)?;
                decode_text_json(txt)
            }
            _ => anyhow::bail!("unknown ws encoding: {}", enc),
        }
    } else {
        // Legacy binary frames are treated as UTF-8 JSON (best effort).
        let txt = std::str::from_utf8(raw)?;
        decode_text_json(txt)
    }
}

fn decode_msgpack(payload: &[u8]) -> Result<Decoded> {
    let wire: WireMessage = rmp_serde::from_slice(payload)?;
    decode_wire(wire)
}

fn decode_wire(wire: WireMessage) -> Result<Decoded> {
    match wire.typ {
        2 | 7 => {
            let fw: MsgpackFileWrite = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::FileWrite(FileWrite {
                path: fw.path,
                etag: fw.etag,
                length: fw.length,
                content: fw.content.map(|b| b.0),
            }))
        }
        6 => {
            let hm: MsgpackHttpMsg = rmp_serde::from_slice(&wire.dat.0)?;
            let syft_url = format!(
                "syft://{}/{}/{}",
                hm.syft_url.datasite, hm.syft_url.app_name, hm.syft_url.endpoint
            );
            Ok(Decoded::Http(HttpMsg {
                syft_url,
                id: hm.id,
                body: hm.body.map(|b| b.0),
            }))
        }
        4 => {
            let ack: MsgpackAck = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::Ack(Ack {
                original_id: ack.original_id,
            }))
        }
        5 => {
            let nack: MsgpackNack = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::Nack(Nack {
                original_id: nack.original_id,
                error: nack.error,
            }))
        }
        8 => {
            let m: MsgpackACLManifest = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::ACLManifest(ACLManifest {
                version: m.version,
                datasite: m.datasite,
                for_user: m.for_user,
                for_hash: m.for_hash,
                generated: m.generated,
                acl_order: m
                    .acl_order
                    .into_iter()
                    .map(|e| ACLEntry {
                        path: e.path,
                        hash: e.hash,
                    })
                    .collect(),
            }))
        }
        9 => {
            let open: MsgpackHotlinkOpen = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::HotlinkOpen(HotlinkOpen {
                session_id: open.session_id,
                path: open.path,
            }))
        }
        10 => {
            let accept: MsgpackHotlinkAccept = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::HotlinkAccept(HotlinkAccept {
                session_id: accept.session_id,
            }))
        }
        11 => {
            let reject: MsgpackHotlinkReject = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::HotlinkReject(HotlinkReject {
                session_id: reject.session_id,
                reason: reject.reason,
            }))
        }
        12 => {
            let hl: MsgpackHotlinkData = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::HotlinkData(HotlinkData {
                session_id: hl.session_id,
                seq: hl.seq,
                path: hl.path,
                etag: hl.etag,
                payload: hl.payload.map(|b| b.0),
            }))
        }
        13 => {
            let close: MsgpackHotlinkClose = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::HotlinkClose(HotlinkClose {
                session_id: close.session_id,
                reason: close.reason,
            }))
        }
        14 => {
            let signal: MsgpackHotlinkSignal = rmp_serde::from_slice(&wire.dat.0)?;
            Ok(Decoded::HotlinkSignal(HotlinkSignal {
                session_id: signal.session_id,
                kind: signal.kind,
                addrs: signal.addrs,
                token: signal.token,
                error: signal.error,
            }))
        }
        _ => Ok(Decoded::Other {
            id: wire.id,
            typ: wire.typ,
        }),
    }
}

fn decode_json_msg(msg: Message) -> Result<Decoded> {
    match msg.typ {
        // MsgFileWrite + MsgFileNotify
        2 | 7 => {
            let fw: JsonFileWrite = serde_json::from_value(msg.dat)?;
            Ok(Decoded::FileWrite(FileWrite {
                path: fw.path,
                etag: fw.etag,
                length: fw.length,
                content: fw.content,
            }))
        }
        // MsgHttp
        6 => {
            let hm: JsonHttpMsg = serde_json::from_value(msg.dat)?;
            Ok(Decoded::Http(HttpMsg {
                syft_url: hm.syft_url,
                id: hm.id,
                body: hm.body,
            }))
        }
        // MsgAck
        4 => {
            let ack: JsonAck = serde_json::from_value(msg.dat)?;
            Ok(Decoded::Ack(Ack {
                original_id: ack.original_id,
            }))
        }
        // MsgNack
        5 => {
            let nack: JsonNack = serde_json::from_value(msg.dat)?;
            Ok(Decoded::Nack(Nack {
                original_id: nack.original_id,
                error: nack.error,
            }))
        }
        // MsgACLManifest
        8 => {
            let m: JsonACLManifest = serde_json::from_value(msg.dat)?;
            Ok(Decoded::ACLManifest(ACLManifest {
                version: m.version,
                datasite: m.datasite,
                for_user: m.for_user,
                for_hash: m.for_hash,
                generated: m.generated,
                acl_order: m
                    .acl_order
                    .into_iter()
                    .map(|e| ACLEntry {
                        path: e.path,
                        hash: e.hash,
                    })
                    .collect(),
            }))
        }
        9 => {
            let open: JsonHotlinkOpen = serde_json::from_value(msg.dat)?;
            Ok(Decoded::HotlinkOpen(HotlinkOpen {
                session_id: open.session_id,
                path: open.path,
            }))
        }
        10 => {
            let accept: JsonHotlinkAccept = serde_json::from_value(msg.dat)?;
            Ok(Decoded::HotlinkAccept(HotlinkAccept {
                session_id: accept.session_id,
            }))
        }
        11 => {
            let reject: JsonHotlinkReject = serde_json::from_value(msg.dat)?;
            Ok(Decoded::HotlinkReject(HotlinkReject {
                session_id: reject.session_id,
                reason: reject.reason,
            }))
        }
        12 => {
            let hl: JsonHotlinkData = serde_json::from_value(msg.dat)?;
            Ok(Decoded::HotlinkData(HotlinkData {
                session_id: hl.session_id,
                seq: hl.seq,
                path: hl.path,
                etag: hl.etag,
                payload: hl.payload,
            }))
        }
        13 => {
            let close: JsonHotlinkClose = serde_json::from_value(msg.dat)?;
            Ok(Decoded::HotlinkClose(HotlinkClose {
                session_id: close.session_id,
                reason: close.reason,
            }))
        }
        14 => {
            let signal: JsonHotlinkSignal = serde_json::from_value(msg.dat)?;
            Ok(Decoded::HotlinkSignal(HotlinkSignal {
                session_id: signal.session_id,
                kind: signal.kind,
                addrs: signal.addrs,
                token: signal.token,
                error: signal.error,
            }))
        }
        _ => Ok(Decoded::Other {
            id: msg.id,
            typ: msg.typ,
        }),
    }
}

fn deserialize_base64_opt<'de, D>(deserializer: D) -> std::result::Result<Option<Vec<u8>>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    let opt = Option::<serde_json::Value>::deserialize(deserializer)?;
    match opt {
        None => Ok(None),
        Some(serde_json::Value::String(s)) => {
            let bytes = base64::engine::general_purpose::STANDARD
                .decode(s.as_bytes())
                .map_err(serde::de::Error::custom)?;
            Ok(Some(bytes))
        }
        Some(serde_json::Value::Array(arr)) => {
            let mut out = Vec::with_capacity(arr.len());
            for v in arr {
                let n = v
                    .as_u64()
                    .ok_or_else(|| serde::de::Error::custom("expected byte"))?;
                out.push(n as u8);
            }
            Ok(Some(out))
        }
        _ => Err(serde::de::Error::custom(
            "expected base64 string or array for bytes",
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn msgpack_ack_nack_field_names_match_go() {
        #[derive(Serialize)]
        struct GoAck {
            #[serde(rename = "OriginalId")]
            original_id: String,
        }

        #[derive(Serialize)]
        struct GoNack {
            #[serde(rename = "OriginalId")]
            original_id: String,
            #[serde(rename = "Error")]
            error: String,
        }

        let ack_bytes = rmp_serde::to_vec_named(&GoAck {
            original_id: "abc".to_string(),
        })
        .unwrap();
        let ack: MsgpackAck = rmp_serde::from_slice(&ack_bytes).unwrap();
        assert_eq!(ack.original_id, "abc");

        let nack_bytes = rmp_serde::to_vec_named(&GoNack {
            original_id: "abc".to_string(),
            error: "nope".to_string(),
        })
        .unwrap();
        let nack: MsgpackNack = rmp_serde::from_slice(&nack_bytes).unwrap();
        assert_eq!(nack.original_id, "abc");
        assert_eq!(nack.error, "nope");
    }

    #[test]
    fn msgpack_envelope_encodes_dat_as_bin() {
        let fw = MsgpackFileWrite {
            path: "alice@example.com/public/x.txt".to_string(),
            etag: "etag".to_string(),
            length: 3,
            content: Some(MpBytes(vec![1, 2, 3])),
        };
        let msg = encode_msgpack("id", 2, &fw).unwrap();
        assert!(msg.len() > 8);
        assert_eq!(msg[0], b'S');
        assert_eq!(msg[1], b'B');
        assert_eq!(msg[2], 1);
        assert_eq!(msg[3], 1); // msgpack

        // Search for the outer wire key "dat" (0xa3 'd' 'a' 't') and assert the following
        // marker is a msgpack bin type (0xc4/0xc5/0xc6), not an array.
        let payload = &msg[4..];
        let mut found = false;
        for i in 0..payload.len().saturating_sub(4) {
            if payload[i] == 0xa3
                && payload[i + 1] == b'd'
                && payload[i + 2] == b'a'
                && payload[i + 3] == b't'
            {
                let marker = payload.get(i + 4).copied().unwrap_or(0);
                assert!(
                    marker == 0xc4 || marker == 0xc5 || marker == 0xc6,
                    "expected bin marker after dat key, got 0x{:02x}",
                    marker
                );
                found = true;
                break;
            }
        }
        assert!(found, "did not find 'dat' key marker in msgpack payload");
    }
}
