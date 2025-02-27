package server

import "github.com/yashgorana/syftbox-go/pkg/blob"

const DefaultAddr = "127.0.0.1:8080"

type Config struct {
	Http *HttpServerConfig
	Blob *blob.BlobConfig
}

type HttpServerConfig struct {
	Addr     string
	CertFile string
	KeyFile  string
}
