package server

import "github.com/yashgorana/syftbox-go/pkg/blob"

const DefaultAddr = "127.0.0.1:8080"

type Config struct {
	Http *HttpServerConfig
	Blob *blob.BlobStorageConfig
}

type HttpServerConfig struct {
	Addr     string
	CertFile string
	KeyFile  string
}

// func NewServerConfig(addr string, certFile string, keyFile string) *ServerConfig {
// 	return &ServerConfig{
// 		Addr:     addr,
// 		CertFile: certFile,
// 		KeyFile:  keyFile,
// 	}
// }

// func DefaultServerConfig() *ServerConfig {
// 	return &ServerConfig{
// 		Addr:     DefaultAddr,
// 		CertFile: "",
// 		KeyFile:  "",
// 	}
// }
