package email

import (
	"context"
)

// Service is the interface for the email service
type Service interface {
	IsEnabled() bool
	Send(ctx context.Context, data *EmailInfo) error
}

type EmailInfo struct {
	FromName  string // Name of the sender
	FromEmail string // Email of the sender
	ToName    string // Name of the recipient
	ToEmail   string // Email of the recipient
	Subject   string // Subject of the email
	HTMLBody  string // HTML body of the email
}
