package send

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/openmined/syftbox/internal/server/blob"
)

var (
	ErrMsgNotFound = errors.New("message not found")
)

type BlobMsgStore struct {
	blob *blob.BlobService
}

func NewBlobMsgStore(blob *blob.BlobService) RPCMsgStore {
	return &BlobMsgStore{blob: blob}
}

func (m *BlobMsgStore) GetMsg(ctx context.Context, path string) (io.ReadCloser, error) {
	// check if it exists in DB first
	_, exists := m.blob.Index().Get(path)
	if !exists {
		return nil, ErrMsgNotFound
	}
	object, err := m.blob.Backend().GetObject(ctx, path)
	if err != nil {
		return nil, err
	}
	return object.Body, nil
}

func (m *BlobMsgStore) DeleteMsg(ctx context.Context, path string) error {
	_, err := m.blob.Backend().DeleteObject(ctx, path)
	return err
}

func (m *BlobMsgStore) StoreMsg(ctx context.Context, path string, msgBytes []byte) error {
	etag := fmt.Sprintf("%x", md5.Sum(msgBytes))
	fileSize := int64(len(msgBytes))

	slog.Info("Storing message", "path", path, "etag", etag, "fileSize", fileSize)

	_, err := m.blob.Backend().PutObject(ctx, &blob.PutObjectParams{
		Key:  path,
		ETag: etag,
		Body: bytes.NewReader(msgBytes),
		Size: fileSize,
	})

	if err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	return nil
}
