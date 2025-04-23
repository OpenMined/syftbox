package client

type Config struct {
	Path        string
	DataDir     string
	Email       string
	ServerURL   string
	AppsEnabled bool
}

type ClientConfig struct {
	Datasite     *DatasiteConfig
	ControlPlane *ControlPlaneConfig
}

type DatasiteConfig struct {
	DataDir     string // datasite data directory
	Email       string // datasite email
	ServerURL   string // datasite server url
	AppsEnabled bool   // enable apps
}

// ControlPlaneConfig contains configuration for the UI bridge server
type ControlPlaneConfig struct {
	Addr          string // Address to bind the UI bridge server
	AuthToken     string // Access token for the UI bridge server
	EnableSwagger bool   // EnableSwagger enables Swagger documentation
}
