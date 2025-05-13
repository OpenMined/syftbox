package handlers

// StatusResponse represents the health status of the service.
type StatusResponse struct {
	Status    string        `json:"status"`    // health status ("ok").
	Timestamp string        `json:"ts"`        // timestamp when health check was performed.
	Version   string        `json:"version"`   // version of the client.
	Revision  string        `json:"revision"`  // revision of the client.
	BuildDate string        `json:"buildDate"` // build date of the client.
	HasConfig bool          `json:"hasConfig"` // whether the datasite has a config.
	Datasite  *DatasiteInfo `json:"datasite"`  // datasite status.
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
