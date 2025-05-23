package server

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/auth"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/email"
)

type Services struct {
	Blob     *blob.BlobService
	ACL      *acl.AclService
	Datasite *datasite.DatasiteService
	Auth     *auth.AuthService
	Email    *email.EmailService
}

func NewServices(config *Config, db *sqlx.DB) (*Services, error) {
	emailSvc := email.NewEmailService(&config.Email)

	blobSvc, err := blob.NewBlobService(&config.Blob, db)
	if err != nil {
		return nil, err
	}

	aclSvc := acl.NewAclService()

	datasiteSvc := datasite.NewDatasiteService(blobSvc, aclSvc)

	authSvc := auth.NewAuthService(&config.Auth, emailSvc)

	return &Services{
		Blob:     blobSvc,
		ACL:      aclSvc,
		Datasite: datasiteSvc,
		Auth:     authSvc,
		Email:    emailSvc,
	}, nil
}

func (s *Services) Start(ctx context.Context) error {
	// first start blob service - it populates the blob index
	if err := s.Blob.Start(ctx); err != nil {
		return fmt.Errorf("start blob service: %w", err)
	}

	// then start datasite service
	if err := s.Datasite.Start(ctx); err != nil {
		return fmt.Errorf("start datasite service: %w", err)
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
