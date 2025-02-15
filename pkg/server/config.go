package server

const DefaultAddr = "127.0.0.1:8080"

type Config struct {
	Http *HttpServerConfig
	Blob *BlobConfig
}

type HttpServerConfig struct {
	Addr     string
	CertFile string
	KeyFile  string
}

type BlobConfig struct {
	BucketName string
	Region     string
	AccessKey  string
	SecretKey  string
	ServerUrl  string
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
