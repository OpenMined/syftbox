package db

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSqliteDB_Memory_Defaults(t *testing.T) {
	database, err := NewSqliteDB()
	require.NoError(t, err)
	defer database.Close()

	// Should be usable.
	_, err = database.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT);")
	require.NoError(t, err)
}

func TestNewSqliteDB_File_CreatesParent(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "nested", "state.db")

	database, err := NewSqliteDB(WithPath(dbPath))
	require.NoError(t, err)
	defer database.Close()

	// Parent dir should exist and db file should be creatable.
	assert.DirExists(t, filepath.Dir(dbPath))
}

func TestNewSqliteDB_CustomPragmas_AllowsOverride(t *testing.T) {
	// SQLite treats unknown pragmas as no-ops, so overriding with a minimal pragma block
	// should still create a usable DB.
	database, err := NewSqliteDB(WithPragmas("PRAGMA journal_mode=WAL;"))
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Exec("CREATE TABLE t2 (id INTEGER PRIMARY KEY);")
	assert.NoError(t, err)
}
