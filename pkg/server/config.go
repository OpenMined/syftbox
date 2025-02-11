package server

const DefaultAddr = "127.0.0.1:8080"

type ServerConfig struct {
	Addr     string
	CertFile string
	KeyFile  string
}

func NewServerConfig(addr string, certFile string, keyFile string) *ServerConfig {
	return &ServerConfig{
		Addr:     addr,
		CertFile: certFile,
		KeyFile:  keyFile,
	}
}

func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Addr:     DefaultAddr,
		CertFile: "",
		KeyFile:  "",
	}
}
