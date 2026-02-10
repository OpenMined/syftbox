package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDaemonCommand_FlagsAndDefaults(t *testing.T) {
	cmd := newDaemonCmd()

	httpAddr := cmd.Flags().Lookup("http-addr")
	require.NotNil(t, httpAddr)
	require.Equal(t, "a", httpAddr.Shorthand)
	require.Equal(t, "localhost:7938", httpAddr.DefValue)

	httpToken := cmd.Flags().Lookup("http-token")
	require.NotNil(t, httpToken)
	require.Equal(t, "t", httpToken.Shorthand)
	require.Equal(t, "", httpToken.DefValue)

	httpSwagger := cmd.Flags().Lookup("http-swagger")
	require.NotNil(t, httpSwagger)
	require.Equal(t, "s", httpSwagger.Shorthand)
	require.Equal(t, "true", httpSwagger.DefValue)
}

