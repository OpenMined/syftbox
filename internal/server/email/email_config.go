package email

import (
	"fmt"
	"log/slog"

	"github.com/openmined/syftbox/internal/utils"
)

type Config struct {
	Enabled        bool   `mapstructure:"enabled"`
	SendgridAPIKey string `mapstructure:"sendgrid_api_key"`
}

func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enabled", c.Enabled),
		slog.String("sendgrid_api_key", utils.MaskSecret(c.SendgridAPIKey)),
	)
}

func (c Config) Validate() error {
	if c.Enabled {
		if c.SendgridAPIKey == "" {
			return fmt.Errorf("sendgrid_api_key is required")
		}
	}
	return nil
}
