package sync3

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

type FileContent struct {
	FileMetadata
	Content []byte
}

func NewFileContent(path string) (*FileContent, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	isDir := stat.IsDir()
	size := stat.Size()
	modTime := stat.ModTime()

	if isDir {
		return nil, fmt.Errorf("path is a directory")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create a buffer to store the file content
	var buffer bytes.Buffer
	// Create an MD5 hasher
	hasher := md5.New()
	// Use MultiWriter to write to both the buffer and the hasher at once
	multiWriter := io.MultiWriter(&buffer, hasher)

	if _, err := io.Copy(multiWriter, file); err != nil {
		return nil, err
	}

	return &FileContent{
		FileMetadata: FileMetadata{
			Size:         size,
			ETag:         fmt.Sprintf("%x", hasher.Sum(nil)),
			Version:      "",
			Path:         path,
			LastModified: modTime,
		},
		Content: buffer.Bytes(),
	}, nil

}
