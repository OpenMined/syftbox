#![allow(dead_code)]

use anyhow::Result;
use tokio::sync::broadcast;
use tokio_tungstenite::tungstenite::protocol::Message;
use url::Url;

#[derive(Clone)]
pub struct Events {
    _url: Url,
    sender: broadcast::Sender<Message>,
}

impl Events {
    pub fn new(base: &str) -> Result<Self> {
        let mut ws = Url::parse(base)?;
        ws.set_scheme("ws").ok();
        ws.set_path("/api/v1/events");
        let (sender, _) = broadcast::channel(32);
        Ok(Self { _url: ws, sender })
    }

    pub async fn connect(&self) -> Result<()> {
        // TODO: implement websocket client; currently unused.
        Ok(())
    }

    pub fn subscribe(&self) -> broadcast::Receiver<Message> {
        self.sender.subscribe()
    }
}
