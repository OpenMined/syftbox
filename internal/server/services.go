package server

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/yashgorana/syftbox-go/internal/server/acl"
	"github.com/yashgorana/syftbox-go/internal/server/blob"
	"github.com/yashgorana/syftbox-go/internal/server/datasite"
)

type Services struct {
	Blob     *blob.BlobService
	ACL      *acl.AclService
	Datasite *datasite.DatasiteService
	// Add other services as needed
}

func NewServices(config *Config, db *sqlx.DB) (*Services, error) {
	aclSvc := acl.NewAclService()

	blobSvc, err := blob.NewBlobService(config.Blob, blob.WithDB(db))
	if err != nil {
		return nil, err
	}

	datasiteSvc := datasite.NewDatasiteService(blobSvc, aclSvc)

	return &Services{
		Blob:     blobSvc,
		ACL:      aclSvc,
		Datasite: datasiteSvc,
	}, nil
}

func (s *Services) Start(ctx context.Context) error {
	if err := s.Datasite.Start(ctx); err != nil {
		return fmt.Errorf("start datasite service: %w", err)
	}

	if err := s.Blob.Start(ctx); err != nil {
		return fmt.Errorf("start blob service: %w", err)
	}

	return nil
}

func (s *Services) Shutdown(ctx context.Context) error {
	if err := s.Blob.Shutdown(ctx); err != nil {
		return fmt.Errorf("stop blob service: %w", err)
	}

	if err := s.Datasite.Shutdown(ctx); err != nil {
		return fmt.Errorf("stop datasite service: %w", err)
	}

	return nil
}
