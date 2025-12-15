
use anyhow::Result;
use base64::Engine;
use serde::{Deserialize, Serialize};

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
    pub content: Option<Vec<u8>>,
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
    body: Option<Vec<u8>>,
}

#[derive(Debug)]
pub enum Decoded {
    FileWrite(FileWrite),
    Http(HttpMsg),
    Ack(Ack),
    Nack(Nack),
    Other { id: String, typ: u16 },
}

#[derive(Debug, Deserialize, Serialize)]
struct WireMessage {
    pub id: String,
    #[serde(rename = "typ")]
    pub typ: u16,
    #[serde(rename = "dat")]
    pub dat: Vec<u8>,
}

pub fn encode_msgpack<T: Serialize>(id: &str, typ: u16, dat: &T) -> Result<Vec<u8>> {
    let dat_bytes = rmp_serde::to_vec_named(dat)?;
    let wire = WireMessage {
        id: id.to_string(),
        typ,
        dat: dat_bytes,
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
            let fw: MsgpackFileWrite = rmp_serde::from_slice(&wire.dat)?;
            Ok(Decoded::FileWrite(FileWrite {
                path: fw.path,
                etag: fw.etag,
                length: fw.length,
                content: fw.content,
            }))
        }
        6 => {
            let hm: MsgpackHttpMsg = rmp_serde::from_slice(&wire.dat)?;
            let syft_url = format!(
                "syft://{}/{}/{}",
                hm.syft_url.datasite, hm.syft_url.app_name, hm.syft_url.endpoint
            );
            Ok(Decoded::Http(HttpMsg {
                syft_url,
                id: hm.id,
                body: hm.body,
            }))
        }
        4 => {
            let ack: MsgpackAck = rmp_serde::from_slice(&wire.dat)?;
            Ok(Decoded::Ack(Ack {
                original_id: ack.original_id,
            }))
        }
        5 => {
            let nack: MsgpackNack = rmp_serde::from_slice(&wire.dat)?;
            Ok(Decoded::Nack(Nack {
                original_id: nack.original_id,
                error: nack.error,
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
