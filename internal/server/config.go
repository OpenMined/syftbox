package server

import "github.com/yashgorana/syftbox-go/internal/blob"

const DefaultAddr = "127.0.0.1:8080"

type Config struct {
	Http   *HttpServerConfig
	Blob   *blob.S3BlobConfig
	DbPath string
}

type HttpServerConfig struct {
	Addr     string
	CertFile string
	KeyFile  string
}
