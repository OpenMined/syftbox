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
	Status string `json:"status"`
	Error  error  `json:"error,omitempty"`
}
