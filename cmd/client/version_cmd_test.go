package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openmined/syftbox/internal/version"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand_PrintsDetailedVersion(t *testing.T) {
	cmd := &cobra.Command{Use: "syftbox"}
	cmd.AddCommand(newVersionCmd())

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})

	require.NoError(t, cmd.Execute())

	got := strings.TrimSpace(out.String())
	require.Equal(t, version.Detailed(), got)
}

