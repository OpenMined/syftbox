package controlplane

// CPServerConfig contains configuration for the control plane server
type CPServerConfig struct {
	Addr          string // Address to bind the control plane server
	AuthToken     string // Access token for the control plane server
	EnableSwagger bool   // EnableSwagger enables Swagger documentation
}
