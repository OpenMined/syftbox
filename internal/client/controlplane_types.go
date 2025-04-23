package client

// ControlPlaneConfig contains configuration for the control plane server
type ControlPlaneConfig struct {
	Addr          string // Address to bind the control plane server
	AuthToken     string // Access token for the control plane server
	EnableSwagger bool   // EnableSwagger enables Swagger documentation
}
