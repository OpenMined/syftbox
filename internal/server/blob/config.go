package blob

import "github.com/jmoiron/sqlx"

type S3BlobConfig struct {
	BucketName    string
	Region        string
	AccessKey     string
	SecretKey     string
	Endpoint      string
	UseAccelerate bool
}

type IndexConfig struct {
	DBPath string
	DB     *sqlx.DB
}
