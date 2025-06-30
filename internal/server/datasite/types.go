package datasite

type BlobURL struct {
	Key string `json:"key"`
	Url string `json:"url"`
}

type BlobError struct {
	Key   string `json:"key"`
	Error string `json:"error"`
}
