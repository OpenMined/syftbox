package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

var (
	ErrEmailDisabled        = errors.New("email is disabled")
	ErrInvalidMailSender    = errors.New("invalid mail sender")
	ErrInvalidMailRecipient = errors.New("invalid mail recipient")
)

type EmailService struct {
	config *Config
}

func NewEmailService(config *Config) *EmailService {
	return &EmailService{config: config}
}

func (s *EmailService) IsEnabled() bool {
	return s.config.Enabled
}

func (s *EmailService) Send(ctx context.Context, data *EmailInfo) error {
	if !s.IsEnabled() {
		slog.Debug("email is disabled, will not send email")
		return ErrEmailDisabled
	}

	if data.FromEmail == "" {
		return ErrInvalidMailSender
	}

	if data.ToEmail == "" {
		return ErrInvalidMailRecipient
	}

	if data.FromName == "" {
		data.FromName = data.FromEmail
	}

	if data.ToName == "" {
		data.ToName = data.ToEmail
	}

	from := mail.NewEmail(data.FromName, data.FromEmail)
	to := mail.NewEmail(data.ToName, data.ToEmail)

	message := mail.NewSingleEmail(from, data.Subject, to, "", data.HTMLBody)
	client := sendgrid.NewSendClient(s.config.SendgridAPIKey)

	resp, err := client.SendWithContext(ctx, message)
	if err != nil {
		slog.Error("failed to send email", "error", err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	slog.Debug("email sent", "to", data.ToEmail, "status", resp.StatusCode, "message", resp.Body, "messageId", resp.Headers["X-Message-Id"])
	return nil
}

var _ Service = (*EmailService)(nil)
