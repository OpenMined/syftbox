package apps

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/require"
)

func makeTestAppZip(t *testing.T, rootDir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_, err := zw.Create(rootDir + "/")
	require.NoError(t, err)

	fh := &zip.FileHeader{
		Name:     rootDir + "/run.sh",
		Method:   zip.Deflate,
		Modified: time.Now(),
	}
	fh.SetMode(0o755)
	w, err := zw.CreateHeader(fh)
	require.NoError(t, err)
	_, err = w.Write([]byte("#!/bin/sh\necho ok\n"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestInstallFromArchive_UsesHTTPClientAndExtractsRunScript(t *testing.T) {
	appsDir := filepath.Join(t.TempDir(), "apps")
	dataDir := filepath.Join(t.TempDir(), "meta")
	mgr := NewManager(appsDir, dataDir)

	repo := "https://github.com/OpenMined/demo-app"
	archiveURL := "https://github.com/OpenMined/demo-app/archive/refs/heads/main.zip"
	zipBytes := makeTestAppZip(t, "demo-app-main")

	orig := syftsdk.HTTPClient
	t.Cleanup(func() { syftsdk.HTTPClient = orig })

	testClient := syftsdk.HTTPClient.Clone().SetCommonRetryCount(0)
	testClient.Transport.WrapRoundTripFunc(func(_ http.RoundTripper) req.HttpRoundTripFunc {
		return func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != archiveURL {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       http.NoBody,
					Header:     make(http.Header),
					Request:    r,
				}, nil
			}
			h := make(http.Header)
			h.Set("Content-Type", "application/zip")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(zipBytes)),
				Header:     h,
				Request:    r,
			}, nil
		}
	})
	syftsdk.HTTPClient = testClient

	app, err := mgr.InstallApp(context.Background(), AppInstallOpts{
		URI:    repo,
		Branch: "main",
		UseGit: false,
		Force:  true,
	})
	require.NoError(t, err)

	require.Equal(t, AppSourceGit, app.Source)
	require.Equal(t, repo, app.SourceURI)
	require.Equal(t, "main", app.Branch)
	require.True(t, utils.FileExists(filepath.Join(app.Path, "run.sh")))
	require.True(t, utils.FileExists(filepath.Join(app.Path, ".syftboxapp.json")))
}
