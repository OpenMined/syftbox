package blob

import "fmt"

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
		return fmt.Errorf("blob `bucket_name` is required")
	}
	if c.Region == "" {
		return fmt.Errorf("blob `region` is required")
	}
	if c.AccessKey == "" {
		return fmt.Errorf("blob `access_key` is required")
	}
	if c.SecretKey == "" {
		return fmt.Errorf("blob `secret_key` is required")
	}
	return nil
}
