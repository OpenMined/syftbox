package datasitemgr

import (
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/datasite"
	"github.com/stretchr/testify/assert"
)

func TestDatasiteManager_NewDefaults(t *testing.T) {
	mgr := New()
	st := mgr.Status()
	assert.Equal(t, DatasiteStatusUnprovisioned, st.Status)
	assert.Nil(t, st.Datasite)
	assert.Nil(t, st.DatasiteError)
}

func TestDatasiteManager_GetConfigPath_DefaultAndOverride(t *testing.T) {
	mgr := New()
	assert.Equal(t, config.DefaultConfigPath, mgr.getConfigPath())

	mgr.SetConfigPath("/tmp/custom.json")
	assert.Equal(t, "/tmp/custom.json", mgr.getConfigPath())
}

func TestDatasiteManager_ProvisionGuards(t *testing.T) {
	mgr := New()

	// Nil config rejected.
	assert.Equal(t, ErrConfigIsNil, mgr.Provision(nil))

	// Already-started rejected.
	mgr.datasite = &datasite.Datasite{}
	assert.Equal(t, ErrDatasiteAlreadyStarted, mgr.Provision(&config.Config{}))
}

func TestDatasiteManager_Get_NotStarted(t *testing.T) {
	mgr := New()
	_, err := mgr.Get()
	assert.Equal(t, ErrDatasiteNotStarted, err)
}

