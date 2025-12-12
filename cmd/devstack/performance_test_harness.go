package main

import (
	"crypto/md5"
	"crypto/rand"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/openmined/syftbox/internal/utils"
)

// DevstackTestHarness provides a complete test environment
type DevstackTestHarness struct {
	t            *testing.T
	root         string
	state        *stackState
	alice        *ClientHelper
	bob          *ClientHelper
	metrics      *MetricsCollector
	cleanup      []func()
	cleanupOnce  sync.Once
	profilesDir  string
	cpuProfile   *os.File
	memProfile   *os.File
	traceFile    *os.File
	blockProfile *os.File
}

// persistentSuite holds a reusable stack when PERF_TEST_SANDBOX is set.
type persistentSuite struct {
	mu          sync.Mutex
	initialized bool
	root        string
	relayRoot   string
	binDir      string
	serverBin   string
	clientBin   string
	emails      []string
	minio       minioState
	server      serverState
	clients     []clientState
	cleanupOnce sync.Once
}

var suite persistentSuite

// ClientHelper wraps a client with test utilities
type ClientHelper struct {
	t         *testing.T
	email     string
	state     clientState
	dataDir   string
	publicDir string
	metrics   *ClientMetrics
}

// ClientMetrics tracks per-client metrics
type ClientMetrics struct {
	mu            sync.Mutex
	filesUploaded int64
	bytesUploaded int64
	uploadErrors  int64
	uploadLatency []time.Duration
}

// MetricsCollector aggregates test metrics
type MetricsCollector struct {
	mu             sync.Mutex
	startTime      time.Time
	latencies      []time.Duration
	throughputMBps []float64
	errors         []error
	cpuSamples     []float64
	memSamples     []uint64
	customMetrics  map[string][]float64
}

// TestReport contains test results
type TestReport struct {
	Duration       time.Duration
	TotalOps       int
	SuccessOps     int
	ErrorOps       int
	P50Latency     time.Duration
	P90Latency     time.Duration
	P95Latency     time.Duration
	P99Latency     time.Duration
	AvgThroughput  float64
	PeakThroughput float64
	ErrorRate      float64
	PeakMemoryMB   uint64
	AvgCPUPercent  float64
}

// NewDevstackHarness creates a test harness with full devstack
func NewDevstackHarness(t *testing.T) *DevstackTestHarness {
	t.Helper()

	// Use persistent sandbox dir if PERF_TEST_SANDBOX env var is set
	if sandboxPath := os.Getenv("PERF_TEST_SANDBOX"); sandboxPath != "" {
		suite.mu.Lock()
		defer suite.mu.Unlock()

		if !suite.initialized {
			if err := suite.initPersistent(t, sandboxPath); err != nil {
				t.Fatalf("init persistent suite: %v", err)
			}
		} else {
			if err := suite.resetForTest(t); err != nil {
				t.Fatalf("reset persistent suite: %v", err)
			}
		}

		state := stackState{
			Root:    suite.root,
			Server:  suite.server,
			Minio:   suite.minio,
			Clients: suite.clients,
			Created: time.Now().UTC(),
		}
		return suite.newHarnessForTest(t, &state)
	}

	// Default (non-persistent) path: create a full fresh stack per test.
	stackRoot := filepath.Join(t.TempDir(), "teststack")
	t.Logf("Using temporary directory: %s", stackRoot)
	state, err := startFullStack(t, stackRoot, true)
	if err != nil {
		t.Fatalf("start full stack: %v", err)
	}
	return newHarnessFromState(t, &state, true)
}

func (s *persistentSuite) initPersistent(t *testing.T, sandboxPath string) error {
	t.Helper()
	rootAbs, err := filepath.Abs(sandboxPath)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	s.root = rootAbs
	s.relayRoot = filepath.Join(s.root, relayDir)
	s.binDir = filepath.Join(s.relayRoot, "bin")
	s.emails = []string{"alice@example.com", "bob@example.com"}

	if err := os.MkdirAll(s.binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	s.serverBin = filepath.Join(s.binDir, "server")
	s.clientBin = filepath.Join(s.binDir, "syftbox")

	repoRoot, err := filepath.Abs(filepath.Join(".", "..", ".."))
	if err != nil {
		return fmt.Errorf("find repo root: %w", err)
	}

	// If a previous devstack for the same sandbox is still running (e.g. from a prior
	// go test invocation), try to reuse its MinIO instead of starting a second one.
	if prev, _, rerr := readState(s.root); rerr == nil && prev != nil {
		if prev.Minio.PID > 0 && processExists(prev.Minio.PID) {
			if err := waitForMinio(prev.Minio.APIPort); err == nil {
				t.Logf("Reusing existing MinIO for persistent suite (pid %d port %d)", prev.Minio.PID, prev.Minio.APIPort)
				s.minio = prev.Minio
			} else {
				t.Logf("Previous MinIO not healthy; stopping and restarting: %v", err)
				stopMinio(prev.Minio)
			}
		}
	}

	t.Logf("Using persistent sandbox: %s", s.root)
	t.Logf("Building binaries once for persistent suite...")
	if err := buildBinary(s.serverBin, filepath.Join(repoRoot, "cmd", "server"), serverBuildTags); err != nil {
		return fmt.Errorf("build server: %w", err)
	}
	if err := buildBinary(s.clientBin, filepath.Join(repoRoot, "cmd", "client"), clientBuildTags); err != nil {
		return fmt.Errorf("build client: %w", err)
	}

	if s.minio.PID == 0 {
		// Allocate ports for MinIO; keep it running across tests.
		minioAPIPort, _ := getFreePort()
		minioConsolePort, _ := getFreePort()
		for minioConsolePort == minioAPIPort {
			minioConsolePort, _ = getFreePort()
		}

		t.Logf("Starting MinIO for persistent suite...")
		minioBin, err := ensureMinioBinary(s.binDir)
		if err != nil {
			return fmt.Errorf("minio binary unavailable: %w", err)
		}
		mState, err := startMinio("local", minioBin, s.relayRoot, minioAPIPort, minioConsolePort, false)
		if err != nil {
			return fmt.Errorf("start minio: %w", err)
		}
		if err := setupBucket(mState.APIPort); err != nil {
			stopMinio(mState)
			return fmt.Errorf("setup bucket: %w", err)
		}
		s.minio = mState
	}

	// Start server+clients fresh for first test.
	if err := s.resetForTest(t); err != nil {
		stopMinio(s.minio)
		return err
	}

	s.initialized = true
	return nil
}

func (s *persistentSuite) resetForTest(t *testing.T) error {
	t.Helper()

	// Stop prior server/clients, if any.
	if s.server.PID > 0 {
		_ = killProcess(s.server.PID)
	}
	for _, c := range s.clients {
		if c.PID > 0 {
			_ = killProcess(c.PID)
		}
	}

	// Wipe server and client state on disk while processes are stopped.
	serverDir := filepath.Join(s.relayRoot, "server")
	_ = os.RemoveAll(serverDir)
	for _, email := range s.emails {
		_ = os.RemoveAll(filepath.Join(s.root, email))
	}

	// Clear MinIO bucket contents for a clean server state.
	// If MinIO isn't healthy (e.g., externally stopped), restart it.
	if err := waitForMinio(s.minio.APIPort); err != nil {
		t.Logf("MinIO not healthy (%v); restarting for persistent suite", err)
		stopMinio(s.minio)
		minioAPIPort, _ := getFreePort()
		minioConsolePort, _ := getFreePort()
		for minioConsolePort == minioAPIPort {
			minioConsolePort, _ = getFreePort()
		}
		minioBin, err2 := ensureMinioBinary(s.binDir)
		if err2 != nil {
			return fmt.Errorf("minio binary unavailable: %w", err2)
		}
		mState, err2 := startMinio("local", minioBin, s.relayRoot, minioAPIPort, minioConsolePort, false)
		if err2 != nil {
			return fmt.Errorf("restart minio: %w", err2)
		}
		if err2 := setupBucket(mState.APIPort); err2 != nil {
			stopMinio(mState)
			return fmt.Errorf("setup bucket after restart: %w", err2)
		}
		s.minio = mState
	}
	if err := emptyBucket(s.minio.APIPort); err != nil {
		return fmt.Errorf("empty bucket: %w", err)
	}

	// Start server and clients using already-built binaries and existing MinIO.
	serverPort, _ := getFreePort()
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	t.Logf("Starting server for test...")
	sState, err := startServer(s.serverBin, s.relayRoot, serverPort, s.minio.APIPort)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	t.Logf("Starting clients for test...")
	var clients []clientState
	for _, email := range s.emails {
		port, _ := getFreePort()
		cState, err := startClient(s.clientBin, s.root, email, serverURL, port)
		if err != nil {
			_ = killProcess(sState.PID)
			return fmt.Errorf("start client %s: %w", email, err)
		}
		clients = append(clients, cState)
	}

	// Write stack state for debugging tools.
	statePath := filepath.Join(s.relayRoot, stateFileName)
	state := stackState{
		Root:    s.root,
		Server:  sState,
		Minio:   s.minio,
		Clients: clients,
		Created: time.Now().UTC(),
	}
	if err := writeState(statePath, &state); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	s.server = sState
	s.clients = clients

	// Wait for initial sync in the fresh state.
	time.Sleep(2 * time.Second)
	t.Logf("Persistent devstack ready: server=%s, alice=pid:%d, bob=pid:%d",
		serverURL, clients[0].PID, clients[1].PID)

	return nil
}

func (s *persistentSuite) newHarnessForTest(t *testing.T, state *stackState) *DevstackTestHarness {
	t.Helper()
	return newHarnessFromState(t, state, false)
}

func startFullStack(t *testing.T, stackRoot string, reset bool) (stackState, error) {
	t.Helper()
	opts := startOptions{
		root:        stackRoot,
		clients:     []string{"alice@example.com", "bob@example.com"},
		randomPorts: true,
		reset:       reset,
	}

	var err error
	opts.root, err = filepath.Abs(opts.root)
	if err != nil {
		return stackState{}, fmt.Errorf("resolve root: %w", err)
	}

	if reset {
		stopStack(opts.root) // best effort
		_ = os.RemoveAll(opts.root)
	}

	// Create directories
	if err := os.MkdirAll(opts.root, 0o755); err != nil {
		return stackState{}, fmt.Errorf("create root dir: %w", err)
	}

	relayRoot := filepath.Join(opts.root, relayDir)
	if err := os.MkdirAll(relayRoot, 0o755); err != nil {
		return stackState{}, fmt.Errorf("create relay dir: %w", err)
	}

	binDir := filepath.Join(relayRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return stackState{}, fmt.Errorf("create bin dir: %w", err)
	}

	// Build binaries
	serverBin := filepath.Join(binDir, "server")
	clientBin := filepath.Join(binDir, "syftbox")

	repoRoot, err := filepath.Abs(filepath.Join(".", "..", ".."))
	if err != nil {
		return stackState{}, fmt.Errorf("find repo root: %w", err)
	}

	t.Logf("Building binaries...")
	if err := buildBinary(serverBin, filepath.Join(repoRoot, "cmd", "server"), serverBuildTags); err != nil {
		return stackState{}, fmt.Errorf("build server: %w", err)
	}
	if err := buildBinary(clientBin, filepath.Join(repoRoot, "cmd", "client"), clientBuildTags); err != nil {
		return stackState{}, fmt.Errorf("build client: %w", err)
	}

	// Allocate ports (ensure MinIO API and console ports differ)
	serverPort, _ := getFreePort()
	minioAPIPort, _ := getFreePort()
	minioConsolePort, _ := getFreePort()
	for minioConsolePort == minioAPIPort {
		minioConsolePort, _ = getFreePort()
	}

	// Start MinIO
	t.Logf("Starting MinIO...")
	minioBin, err := ensureMinioBinary(binDir)
	if err != nil {
		return stackState{}, fmt.Errorf("minio binary unavailable: %w", err)
	}

	mState, err := startMinio("local", minioBin, relayRoot, minioAPIPort, minioConsolePort, false)
	if err != nil {
		return stackState{}, fmt.Errorf("start minio: %w", err)
	}

	// Setup bucket
	if err := setupBucket(mState.APIPort); err != nil {
		stopMinio(mState)
		return stackState{}, fmt.Errorf("setup bucket: %w", err)
	}

	// Start server
	t.Logf("Starting server...")
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	sState, err := startServer(serverBin, relayRoot, serverPort, mState.APIPort)
	if err != nil {
		stopMinio(mState)
		return stackState{}, fmt.Errorf("start server: %w", err)
	}

	// Start clients
	t.Logf("Starting clients...")
	var clients []clientState
	for _, email := range opts.clients {
		port, _ := getFreePort()
		cState, err := startClient(clientBin, opts.root, email, serverURL, port)
		if err != nil {
			_ = killProcess(sState.PID)
			stopMinio(mState)
			return stackState{}, fmt.Errorf("start client %s: %w", email, err)
		}
		clients = append(clients, cState)
	}

	// Write state
	statePath := filepath.Join(relayRoot, stateFileName)
	state := stackState{
		Root:    opts.root,
		Server:  sState,
		Minio:   mState,
		Clients: clients,
		Created: time.Now().UTC(),
	}
	if err := writeState(statePath, &state); err != nil {
		return stackState{}, fmt.Errorf("write state: %w", err)
	}

	// Wait for initial sync
	time.Sleep(2 * time.Second)
	return state, nil
}

func newHarnessFromState(t *testing.T, state *stackState, ownProcesses bool) *DevstackTestHarness {
	t.Helper()
	emails := []string{"alice@example.com", "bob@example.com"}
	clients := state.Clients

	aliceHelper := &ClientHelper{
		t:         t,
		email:     emails[0],
		state:     clients[0],
		dataDir:   clients[0].DataPath,
		publicDir: filepath.Join(clients[0].DataPath, "datasites", emails[0], "public"),
		metrics:   &ClientMetrics{},
	}

	bobHelper := &ClientHelper{
		t:         t,
		email:     emails[1],
		state:     clients[1],
		dataDir:   clients[1].DataPath,
		publicDir: filepath.Join(clients[1].DataPath, "datasites", emails[1], "public"),
		metrics:   &ClientMetrics{},
	}

	metrics := &MetricsCollector{
		startTime:     time.Now(),
		customMetrics: make(map[string][]float64),
	}

	harness := &DevstackTestHarness{
		t:       t,
		root:    state.Root,
		state:   state,
		alice:   aliceHelper,
		bob:     bobHelper,
		metrics: metrics,
	}

	if ownProcesses {
		sState := state.Server
		mState := state.Minio
		harness.cleanup = []func(){
			func() { _ = killProcess(sState.PID) },
			func() { stopMinio(mState) },
			func() { _ = killProcess(clients[0].PID) },
			func() { _ = killProcess(clients[1].PID) },
			func() {
				if os.Getenv("PERF_TEST_SANDBOX") == "" {
					_ = os.RemoveAll(state.Root)
				}
			},
		}

		t.Cleanup(func() {
			if err := harness.StopProfiling(); err != nil {
				t.Logf("Warning: failed to stop profiling: %v", err)
			}
			harness.Cleanup()
		})
	} else {
		// Persistent mode: do not stop shared processes per test.
		t.Cleanup(func() {
			if err := harness.StopProfiling(); err != nil {
				t.Logf("Warning: failed to stop profiling: %v", err)
			}
		})
	}

	return harness
}

// emptyBucket deletes all objects in the default bucket, leaving MinIO running.
func emptyBucket(apiPort int) error {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(defaultMinioAdminUser, defaultMinioAdminPassword, ""),
		),
		config.WithRegion(defaultRegion),
	)
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	// Ensure bucket exists.
	_, err = client.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: aws.String(defaultBucket),
	})
	if err != nil && !isBucketExistsError(err) {
		return err
	}

	// List + delete in batches.
	var continuation *string
	for {
		out, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
			Bucket:            aws.String(defaultBucket),
			ContinuationToken: continuation,
		})
		if err != nil {
			return err
		}
		if len(out.Contents) == 0 {
			break
		}
		var objs []s3types.ObjectIdentifier
		for _, c := range out.Contents {
			if c.Key != nil {
				objs = append(objs, s3types.ObjectIdentifier{Key: c.Key})
			}
		}
		_, err = client.DeleteObjects(context.Background(), &s3.DeleteObjectsInput{
			Bucket: aws.String(defaultBucket),
			Delete: &s3types.Delete{Objects: objs, Quiet: aws.Bool(true)},
		})
		if err != nil {
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) {
				return fmt.Errorf("delete objects: %s: %w", apiErr.ErrorCode(), err)
			}
			return err
		}
		if aws.ToBool(out.IsTruncated) && out.NextContinuationToken != nil {
			continuation = out.NextContinuationToken
			continue
		}
		break
	}
	return nil
}

// Cleanup stops all processes and cleans up
func (h *DevstackTestHarness) Cleanup() {
	h.cleanupOnce.Do(func() {
		h.t.Logf("Cleaning up devstack...")
		for _, fn := range h.cleanup {
			fn()
		}
	})
}

// UploadFile creates and uploads a file from a client
func (c *ClientHelper) UploadFile(relPath string, content []byte) error {
	c.t.Helper()

	start := time.Now()

	// Write file to client's datasite
	fullPath := filepath.Join(c.publicDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		c.metrics.mu.Lock()
		c.metrics.uploadErrors++
		c.metrics.mu.Unlock()
		return fmt.Errorf("write file: %w", err)
	}

	// Wait for file to sync (trigger priority or wait for next cycle)
	time.Sleep(100 * time.Millisecond)

	c.metrics.mu.Lock()
	c.metrics.filesUploaded++
	c.metrics.bytesUploaded += int64(len(content))
	c.metrics.uploadLatency = append(c.metrics.uploadLatency, time.Since(start))
	c.metrics.mu.Unlock()

	c.t.Logf("%s uploaded %s (%d bytes)", c.email, relPath, len(content))
	return nil
}

// WaitForFile waits for a file to appear in the client's datasite
func (c *ClientHelper) WaitForFile(senderEmail, relPath string, expectedMD5 string, timeout time.Duration) error {
	c.t.Helper()

	// File syncs to datasites/{sender}/public/{relPath}
	fullPath := filepath.Join(c.dataDir, "datasites", senderEmail, "public", relPath)
	var lastMD5 string

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			content, err := os.ReadFile(fullPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("read file: %w", err)
			}

			if expectedMD5 == "" {
				c.t.Logf("%s received %s from %s", c.email, relPath, senderEmail)
				return nil
			}

			actualMD5 := fmt.Sprintf("%x", md5.Sum(content))
			if actualMD5 == expectedMD5 {
				c.t.Logf("%s received %s from %s", c.email, relPath, senderEmail)
				return nil
			}

			if actualMD5 != lastMD5 {
				// File arrived but is stale; keep waiting for the updated version.
				c.t.Logf("waiting for %s from %s: have md5 %s, want %s", relPath, senderEmail, actualMD5, expectedMD5)
				lastMD5 = actualMD5
			}
		}
	}

	return fmt.Errorf("timeout waiting for file %s (last seen MD5=%s)", fullPath, lastMD5)
}

// CreateDefaultACLs creates the default root and public ACL files like the real client
func (c *ClientHelper) CreateDefaultACLs() error {
	c.t.Helper()

	// Root directory is datasites/{email}/
	userDir := filepath.Join(c.dataDir, "datasites", c.email)
	publicDir := filepath.Join(userDir, "public")

	// Create root ACL with private access (owner only via implicit isOwner check)
	rootACLPath := filepath.Join(userDir, "syft.pub.yaml")
	if _, err := os.Stat(rootACLPath); os.IsNotExist(err) {
		rootACL := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: []
      read: []
`
		if err := os.MkdirAll(userDir, 0o755); err != nil {
			return fmt.Errorf("create user dir: %w", err)
		}
		if err := os.WriteFile(rootACLPath, []byte(rootACL), 0o644); err != nil {
			return fmt.Errorf("create root ACL: %w", err)
		}
		c.t.Logf("%s created root ACL", c.email)
	}

	// Create public ACL with public read/write access
	publicACLPath := filepath.Join(publicDir, "syft.pub.yaml")
	if _, err := os.Stat(publicACLPath); os.IsNotExist(err) {
		publicACL := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['*']
      read: ['*']
`
		if err := os.MkdirAll(publicDir, 0o755); err != nil {
			return fmt.Errorf("create public dir: %w", err)
		}
		if err := os.WriteFile(publicACLPath, []byte(publicACL), 0o644); err != nil {
			return fmt.Errorf("create public ACL: %w", err)
		}
		c.t.Logf("%s created public ACL", c.email)
	}

	return nil
}

// GetRPCPath returns the RPC directory path for a given app and endpoint
func (c *ClientHelper) GetRPCPath(appName, endpoint string) (string, error) {
	c.t.Helper()

	syftURL, err := utils.NewSyftBoxURL(c.email, appName, endpoint)
	if err != nil {
		return "", fmt.Errorf("create SyftBoxURL: %w", err)
	}

	return filepath.Join(c.dataDir, "datasites", syftURL.ToLocalPath()), nil
}

// SetupRPCEndpoint creates the RPC directory structure and ACL file
func (c *ClientHelper) SetupRPCEndpoint(appName, endpoint string) error {
	c.t.Helper()

	rpcPath, err := c.GetRPCPath(appName, endpoint)
	if err != nil {
		return err
	}

	// Create directory
	if err := os.MkdirAll(rpcPath, 0o755); err != nil {
		return fmt.Errorf("create rpc dir: %w", err)
	}

	// Ensure an app-level RPC ACL exists so peers can discover endpoints before any requests.
	rpcRootACL := `rules:
  - pattern: '**.request'
    access:
      admin: []
      read:
        - '*'
      write:
        - '*'
  - pattern: '**.response'
    access:
      admin: []
      read:
        - '*'
      write:
        - '*'
`
	rpcRootPath := filepath.Join(filepath.Dir(rpcPath), "syft.pub.yaml")
	if err := os.MkdirAll(filepath.Dir(rpcRootPath), 0o755); err != nil {
		return fmt.Errorf("create rpc root dir: %w", err)
	}
	if err := os.WriteFile(rpcRootPath, []byte(rpcRootACL), 0o644); err != nil {
		return fmt.Errorf("write rpc root acl: %w", err)
	}

	// Create ACL file for RPC endpoint
	// Server will broadcast ACL files without permission checks (they're metadata)
	// Use '**.request' to match files at any depth including same directory
	aclContent := `rules:
  - pattern: '**.request'
    access:
      admin: []
      read:
        - '*'
      write:
        - '*'
  - pattern: '**.response'
    access:
      admin: []
      read:
        - '*'
      write:
        - '*'
`
	aclPath := filepath.Join(rpcPath, "syft.pub.yaml")
	if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
		return fmt.Errorf("write acl file: %w", err)
	}

	c.t.Logf("%s setup RPC endpoint: %s/%s", c.email, appName, endpoint)
	return nil
}

// UploadRPCRequest uploads a request file to the RPC endpoint
func (c *ClientHelper) UploadRPCRequest(appName, endpoint, filename string, content []byte) error {
	c.t.Helper()

	start := time.Now()

	rpcPath, err := c.GetRPCPath(appName, endpoint)
	if err != nil {
		c.metrics.mu.Lock()
		c.metrics.uploadErrors++
		c.metrics.mu.Unlock()
		return err
	}

	fullPath := filepath.Join(rpcPath, filename)

	c.t.Logf("DEBUG: Writing file to: %s", fullPath)

	// Write request file
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		c.metrics.mu.Lock()
		c.metrics.uploadErrors++
		c.metrics.mu.Unlock()
		return fmt.Errorf("write request: %w", err)
	}

	// File watcher should detect and trigger WebSocket sync

	c.metrics.mu.Lock()
	c.metrics.filesUploaded++
	c.metrics.bytesUploaded += int64(len(content))
	c.metrics.uploadLatency = append(c.metrics.uploadLatency, time.Since(start))
	c.metrics.mu.Unlock()

	c.t.Logf("%s uploaded RPC request %s/%s/%s", c.email, appName, endpoint, filename)
	return nil
}

// WaitForRPCRequest waits for an RPC request file to sync
func (c *ClientHelper) WaitForRPCRequest(senderEmail, appName, endpoint, filename, expectedMD5 string, timeout time.Duration) error {
	c.t.Helper()

	// Bob receives at: {bob's datasites dir}/{alice's datasite}/app_data/{appname}/rpc/{endpoint}/{filename}
	fullPath := filepath.Join(
		c.dataDir,
		"datasites",
		senderEmail,
		"app_data",
		appName,
		"rpc",
		endpoint,
		filename,
	)

	c.t.Logf("DEBUG: Waiting for file at: %s", fullPath)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond) // Faster polling for WebSocket
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			if _, err := os.Stat(fullPath); err == nil {
				// File exists, verify MD5
				if expectedMD5 != "" {
					content, err := os.ReadFile(fullPath)
					if err != nil {
						return fmt.Errorf("read file: %w", err)
					}

					actualMD5 := fmt.Sprintf("%x", md5.Sum(content))
					if actualMD5 != expectedMD5 {
						return fmt.Errorf("MD5 mismatch: expected %s, got %s", expectedMD5, actualMD5)
					}
				}

				c.t.Logf("%s received RPC request from %s: %s/%s/%s", c.email, senderEmail, appName, endpoint, filename)
				return nil
			}
		}
	}

	return fmt.Errorf("timeout waiting for RPC request: %s", fullPath)
}

// GenerateRandomFile creates random file content
func GenerateRandomFile(size int) []byte {
	data := make([]byte, size)
	_, _ = rand.Read(data)
	return data
}

// CalculateMD5 computes MD5 hash of data
func CalculateMD5(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}

// RecordLatency records an operation latency
func (m *MetricsCollector) RecordLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencies = append(m.latencies, d)
}

// RecordError records an error
func (m *MetricsCollector) RecordError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, err)
}

// RecordThroughput records throughput in MB/s
func (m *MetricsCollector) RecordThroughput(mbps float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.throughputMBps = append(m.throughputMBps, mbps)
}

// RecordCustomMetric records a custom metric
func (m *MetricsCollector) RecordCustomMetric(name string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.customMetrics[name] == nil {
		m.customMetrics[name] = make([]float64, 0)
	}
	m.customMetrics[name] = append(m.customMetrics[name], value)
}

// GenerateReport creates a test report
func (m *MetricsCollector) GenerateReport() *TestReport {
	m.mu.Lock()
	defer m.mu.Unlock()

	report := &TestReport{
		Duration:   time.Since(m.startTime),
		TotalOps:   len(m.latencies),
		SuccessOps: len(m.latencies) - len(m.errors),
		ErrorOps:   len(m.errors),
	}

	if len(m.latencies) > 0 {
		report.P50Latency = percentile(m.latencies, 0.50)
		report.P90Latency = percentile(m.latencies, 0.90)
		report.P95Latency = percentile(m.latencies, 0.95)
		report.P99Latency = percentile(m.latencies, 0.99)
	}

	if len(m.throughputMBps) > 0 {
		var sum float64
		var peak float64
		for _, v := range m.throughputMBps {
			sum += v
			if v > peak {
				peak = v
			}
		}
		report.AvgThroughput = sum / float64(len(m.throughputMBps))
		report.PeakThroughput = peak
	}

	if report.TotalOps > 0 {
		report.ErrorRate = float64(report.ErrorOps) / float64(report.TotalOps)
	}

	return report
}

// percentile calculates the p-th percentile of durations
func percentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	// Simple implementation - should use proper sorting for production
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)

	// Sort manually (bubble sort for simplicity)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// LogReport logs a test report
func (r *TestReport) Log(t *testing.T) {
	t.Helper()

	t.Logf("=== Test Report ===")
	t.Logf("Duration:         %v", r.Duration)
	t.Logf("Total Operations: %d", r.TotalOps)
	t.Logf("Success:          %d", r.SuccessOps)
	t.Logf("Errors:           %d (%.2f%%)", r.ErrorOps, r.ErrorRate*100)
	t.Logf("Latency P50:      %v", r.P50Latency)
	t.Logf("Latency P90:      %v", r.P90Latency)
	t.Logf("Latency P95:      %v", r.P95Latency)
	t.Logf("Latency P99:      %v", r.P99Latency)
	if r.AvgThroughput > 0 {
		t.Logf("Avg Throughput:   %.2f MB/s", r.AvgThroughput)
		t.Logf("Peak Throughput:  %.2f MB/s", r.PeakThroughput)
	}
	t.Logf("==================")
}

// StartProfiling starts CPU, memory, and trace profiling
func (h *DevstackTestHarness) StartProfiling(testName string) error {
	if os.Getenv("PERF_PROFILE") == "" {
		return nil // Profiling disabled
	}

	h.profilesDir = filepath.Join("profiles", testName)
	if err := os.MkdirAll(h.profilesDir, 0o755); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}

	// CPU profiling
	cpuFile := filepath.Join(h.profilesDir, "cpu.prof")
	f, err := os.Create(cpuFile)
	if err != nil {
		return fmt.Errorf("create cpu profile: %w", err)
	}
	h.cpuProfile = f
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return fmt.Errorf("start cpu profile: %w", err)
	}

	// Execution trace
	traceFile := filepath.Join(h.profilesDir, "trace.out")
	f, err = os.Create(traceFile)
	if err != nil {
		return fmt.Errorf("create trace file: %w", err)
	}
	h.traceFile = f
	if err := trace.Start(f); err != nil {
		f.Close()
		return fmt.Errorf("start trace: %w", err)
	}

	// Enable block profiling
	runtime.SetBlockProfileRate(1)

	h.t.Logf("📊 Profiling enabled: %s", h.profilesDir)
	return nil
}

// StopProfiling stops all profiling and writes profile data
func (h *DevstackTestHarness) StopProfiling() error {
	if os.Getenv("PERF_PROFILE") == "" {
		return nil
	}

	// Stop CPU profiling
	if h.cpuProfile != nil {
		pprof.StopCPUProfile()
		h.cpuProfile.Close()
		h.t.Logf("✅ CPU profile: %s/cpu.prof", h.profilesDir)
	}

	// Stop trace
	if h.traceFile != nil {
		trace.Stop()
		h.traceFile.Close()
		h.t.Logf("✅ Trace file: %s/trace.out", h.profilesDir)
	}

	// Write heap profile
	heapFile := filepath.Join(h.profilesDir, "heap.prof")
	f, err := os.Create(heapFile)
	if err != nil {
		return fmt.Errorf("create heap profile: %w", err)
	}
	defer f.Close()
	runtime.GC() // Get up-to-date statistics
	if err := pprof.WriteHeapProfile(f); err != nil {
		return fmt.Errorf("write heap profile: %w", err)
	}
	h.t.Logf("✅ Heap profile: %s/heap.prof", h.profilesDir)

	// Write goroutine profile
	goroutineFile := filepath.Join(h.profilesDir, "goroutine.prof")
	f, err = os.Create(goroutineFile)
	if err != nil {
		return fmt.Errorf("create goroutine profile: %w", err)
	}
	defer f.Close()
	if err := pprof.Lookup("goroutine").WriteTo(f, 0); err != nil {
		return fmt.Errorf("write goroutine profile: %w", err)
	}
	h.t.Logf("✅ Goroutine profile: %s/goroutine.prof", h.profilesDir)

	// Write block profile
	blockFile := filepath.Join(h.profilesDir, "block.prof")
	f, err = os.Create(blockFile)
	if err != nil {
		return fmt.Errorf("create block profile: %w", err)
	}
	defer f.Close()
	if err := pprof.Lookup("block").WriteTo(f, 0); err != nil {
		return fmt.Errorf("write block profile: %w", err)
	}
	h.t.Logf("✅ Block profile: %s/block.prof", h.profilesDir)

	// Write mutex profile
	mutexFile := filepath.Join(h.profilesDir, "mutex.prof")
	f, err = os.Create(mutexFile)
	if err != nil {
		return fmt.Errorf("create mutex profile: %w", err)
	}
	defer f.Close()
	runtime.SetMutexProfileFraction(1)
	if err := pprof.Lookup("mutex").WriteTo(f, 0); err != nil {
		return fmt.Errorf("write mutex profile: %w", err)
	}
	h.t.Logf("✅ Mutex profile: %s/mutex.prof", h.profilesDir)

	h.t.Logf("\n📊 View profiles:")
	h.t.Logf("  CPU flame graph:    go tool pprof -http=:8080 %s/cpu.prof", h.profilesDir)
	h.t.Logf("  Heap analysis:      go tool pprof -http=:8080 %s/heap.prof", h.profilesDir)
	h.t.Logf("  Execution trace:    go tool trace %s/trace.out", h.profilesDir)
	h.t.Logf("  Goroutines:         go tool pprof -http=:8080 %s/goroutine.prof", h.profilesDir)
	h.t.Logf("  Lock contention:    go tool pprof -http=:8080 %s/block.prof", h.profilesDir)

	return nil
}
