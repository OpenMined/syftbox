package handlers

// StatusResponse represents the health status of the service.
type StatusResponse struct {
	Status    string        `json:"status"`    // health status ("ok").
	Timestamp string        `json:"ts"`        // timestamp when health check was performed.
	Version   string        `json:"version"`   // version of the client.
	Revision  string        `json:"revision"`  // revision of the client.
	BuildDate string        `json:"buildDate"` // build date of the client.
	Datasite  *DatasiteInfo `json:"datasite"`  // datasite status.
	Runtime   *RuntimeInfo  `json:"runtime,omitempty"`
}

type DatasiteInfo struct {
	Status string          `json:"status"`           // status of the datasite.
	Error  string          `json:"error,omitempty"`  // error message if the datasite is not ready.
	Config *DatasiteConfig `json:"config,omitempty"` // config of the datasite.
}

type DatasiteConfig struct {
	DataDir   string `json:"data_dir"`
	Email     string `json:"email"`
	ServerURL string `json:"server_url"`
}

// RuntimeInfo aggregates frequently-polled operational stats.
type RuntimeInfo struct {
	Client    *ClientInfo    `json:"client,omitempty"`
	Websocket *WebsocketInfo `json:"websocket,omitempty"`
	HTTP      *HTTPInfo      `json:"http,omitempty"`
	Sync      *SyncInfo      `json:"sync,omitempty"`
	Uploads   *UploadsInfo   `json:"uploads,omitempty"`
}

type ClientInfo struct {
	Version      string `json:"version"`
	Revision     string `json:"revision"`
	BuildDate    string `json:"build_date"`
	StartedAt    string `json:"started_at"`
	UptimeSec    int64  `json:"uptime_sec"`
	ServerURL    string `json:"server_url,omitempty"`
	ClientURL    string `json:"client_url,omitempty"`
	ClientToken  bool   `json:"client_token_configured"`
}

type WebsocketInfo struct {
	Connected        bool   `json:"connected"`
	Encoding         string `json:"encoding,omitempty"`
	ReconnectAttempt int    `json:"reconnect_attempt"`
	Reconnects       int64  `json:"reconnects"`
	TxQueueLen       int    `json:"tx_queue_len"`
	RxQueueLen       int    `json:"rx_queue_len"`
	OverflowQueueLen int    `json:"overflow_queue_len"`
	BytesSentTotal   int64  `json:"bytes_sent_total"`
	BytesRecvTotal   int64  `json:"bytes_recv_total"`
	ConnectedAt      string `json:"connected_at,omitempty"`
	DisconnectedAt   string `json:"disconnected_at,omitempty"`
	LastSentAt       string `json:"last_sent_at,omitempty"`
	LastRecvAt       string `json:"last_recv_at,omitempty"`
	LastPingAt       string `json:"last_ping_at,omitempty"`
	LastError        string `json:"last_error,omitempty"`
}

type HTTPInfo struct {
	BytesSentTotal int64  `json:"bytes_sent_total"`
	BytesRecvTotal int64  `json:"bytes_recv_total"`
	LastSentAt     string `json:"last_sent_at,omitempty"`
	LastRecvAt     string `json:"last_recv_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

type SyncInfo struct {
	LastFullSyncAt    string `json:"last_full_sync_at,omitempty"`
	TrackedFiles      int    `json:"tracked_files"`
	SyncingFiles      int    `json:"syncing_files"`
	ConflictedFiles   int    `json:"conflicted_files"`
	RejectedFiles     int    `json:"rejected_files"`
}

type UploadsInfo struct {
	Total     int `json:"total"`
	Uploading int `json:"uploading"`
	Pending   int `json:"pending"`
	Paused    int `json:"paused"`
	Error     int `json:"error"`
}
