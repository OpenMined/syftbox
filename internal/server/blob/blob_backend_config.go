package blob

import (
	"fmt"
	"log/slog"

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

func (s3c S3Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("bucket_name", s3c.BucketName),
		slog.String("region", s3c.Region),
		slog.String("endpoint", s3c.Endpoint),
		slog.String("access_key", utils.MaskSecret(s3c.AccessKey)),
		slog.String("secret_key", utils.MaskSecret(s3c.SecretKey)),
		slog.Bool("use_accelerate", s3c.UseAccelerate),
	)
}
