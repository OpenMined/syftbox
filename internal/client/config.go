package client

import "github.com/yashgorana/syftbox-go/internal/localhttp"

type Config struct {
	Path        string
	DataDir     string
	Email       string
	ServerURL   string
	AppsEnabled bool
	UIBridge    localhttp.Config
}
