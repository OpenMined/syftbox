package blob

type S3Config struct {
	BucketName    string
	Region        string
	AccessKey     string
	SecretKey     string
	Endpoint      string
	UseAccelerate bool
}
