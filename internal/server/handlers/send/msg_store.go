package send

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/syftmsg"
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

func (m *BlobMsgStore) StoreMsg(ctx context.Context, path string, msg syftmsg.SyftRPCMessage) error {
	msgBytes, err := msg.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	_, err = m.blob.Backend().PutObject(ctx, &blob.PutObjectParams{
		Key:  path,
		ETag: msg.ID.String(),
		Body: bytes.NewReader(msgBytes),
		Size: int64(len(msgBytes)),
	})
	return err
}
