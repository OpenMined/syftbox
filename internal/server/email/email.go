package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

var (
	ErrKeyMissing           = errors.New("sendgrid api key is not set")
	ErrInvalidMailSender    = errors.New("invalid mail sender")
	ErrInvalidMailRecipient = errors.New("invalid mail recipient")
)

func Send(ctx context.Context, data *EmailData) error {
	sendgridApiKey := os.Getenv("SENDGRID_API_KEY")

	if sendgridApiKey == "" {
		return ErrKeyMissing
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
	client := sendgrid.NewSendClient(sendgridApiKey)

	resp, err := client.SendWithContext(ctx, message)
	if err != nil {
		slog.Error("failed to send email", "error", err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	slog.Debug("email sent", "to", data.ToEmail, "status", resp.StatusCode, "message", resp.Body, "messageId", resp.Headers["X-Message-Id"])
	return nil
}
