package subscriptions

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionForPath_DefaultsAndOwner(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Defaults: Defaults{
			Action: ActionBlock,
		},
		Rules: []Rule{
			{Action: ActionAllow, Datasite: "bob@example.com", Path: "public/**"},
			{Action: ActionPause, Path: "carol@example.com/shared/**"},
		},
	}

	assert.Equal(t, ActionAllow, cfg.ActionForPath("alice@example.com", "alice@example.com/private/a.txt"))
	assert.Equal(t, ActionAllow, cfg.ActionForPath("", "bob@example.com/public/a.txt"))
	assert.Equal(t, ActionPause, cfg.ActionForPath("", "carol@example.com/shared/a.txt"))
	assert.Equal(t, ActionBlock, cfg.ActionForPath("", "bob@example.com/private/a.txt"))
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	cfg := &Config{
		Version: 1,
		Defaults: Defaults{
			Action: ActionPause,
		},
		Rules: []Rule{
			{Action: ActionAllow, Datasite: "*@example.com", Path: "public/**"},
		},
	}

	require.NoError(t, Save(path, cfg))
	loaded, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, cfg.Version, loaded.Version)
	assert.Equal(t, cfg.Defaults.Action, loaded.Defaults.Action)
	assert.Len(t, loaded.Rules, 1)
	assert.Equal(t, "public/**", loaded.Rules[0].Path)
}
