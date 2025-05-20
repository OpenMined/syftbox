package blob

import (
	"fmt"

	"github.com/openmined/syftbox/internal/utils"
)

type S3Config struct {
	BucketName    string `mapstructure:"bucket_name"`
	Region        string `mapstructure:"region"`
	AccessKey     string `mapstructure:"access_key"`
	SecretKey     string `mapstructure:"secret_key"`
	Endpoint      string `mapstructure:"endpoint"`
	UseAccelerate bool   `mapstructure:"use_accelerate"`
}

func (c *S3Config) Validate() error {
	if c.BucketName == "" {
		return fmt.Errorf("bucket_name required")
	}
	if c.Region == "" {
		return fmt.Errorf("region required")
	}
	if c.AccessKey == "" {
		return fmt.Errorf("access_key required")
	}
	if c.SecretKey == "" {
		return fmt.Errorf("secret_key required")
	}
	if c.Endpoint != "" && !utils.IsValidURL(c.Endpoint) {
		return fmt.Errorf("invalid endpoint URL %q", c.Endpoint)
	}
	return nil
}
