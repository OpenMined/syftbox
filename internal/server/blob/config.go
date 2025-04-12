package blob

type S3BlobConfig struct {
	BucketName    string
	Region        string
	AccessKey     string
	SecretKey     string
	Endpoint      string
	UseAccelerate bool
}
