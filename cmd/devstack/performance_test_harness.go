package main

import (
	"crypto/md5"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"testing"
	"time"

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
	var stackRoot string
	if sandboxPath := os.Getenv("PERF_TEST_SANDBOX"); sandboxPath != "" {
		stackRoot = sandboxPath
		t.Logf("Using persistent sandbox: %s", stackRoot)
	} else {
		tmpDir := t.TempDir()
		stackRoot = filepath.Join(tmpDir, "teststack")
		t.Logf("Using temporary directory: %s", stackRoot)
	}

	emails := []string{"alice@example.com", "bob@example.com"}

	opts := startOptions{
		root:        stackRoot,
		clients:     emails,
		randomPorts: true,
		reset:       true,
	}

	var err error
	opts.root, err = filepath.Abs(opts.root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}

	// Create directories
	if err := os.MkdirAll(opts.root, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}

	relayRoot := filepath.Join(opts.root, relayDir)
	if err := os.MkdirAll(relayRoot, 0o755); err != nil {
		t.Fatalf("create relay dir: %v", err)
	}

	binDir := filepath.Join(relayRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	// Build binaries
	serverBin := filepath.Join(binDir, "server")
	clientBin := filepath.Join(binDir, "syftbox")

	repoRoot, err := filepath.Abs(filepath.Join(".", "..", ".."))
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	t.Logf("Building binaries...")
	if err := buildBinary(serverBin, filepath.Join(repoRoot, "cmd", "server"), serverBuildTags); err != nil {
		t.Fatalf("build server: %v", err)
	}
	if err := buildBinary(clientBin, filepath.Join(repoRoot, "cmd", "client"), clientBuildTags); err != nil {
		t.Fatalf("build client: %v", err)
	}

	// Allocate ports
	serverPort, _ := getFreePort()
	minioAPIPort, _ := getFreePort()
	minioConsolePort, _ := getFreePort()

	// Start MinIO
	t.Logf("Starting MinIO...")
	minioBin, err := ensureMinioBinary(binDir)
	if err != nil {
		t.Fatalf("minio binary unavailable: %v", err)
	}

	mState, err := startMinio("local", minioBin, relayRoot, minioAPIPort, minioConsolePort, false)
	if err != nil {
		t.Fatalf("start minio: %v", err)
	}

	// Setup bucket
	if err := setupBucket(mState.APIPort); err != nil {
		stopMinio(mState)
		t.Fatalf("setup bucket: %v", err)
	}

	// Start server
	t.Logf("Starting server...")
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	sState, err := startServer(serverBin, relayRoot, serverPort, mState.APIPort)
	if err != nil {
		stopMinio(mState)
		t.Fatalf("start server: %v", err)
	}

	// Start clients
	t.Logf("Starting clients...")
	var clients []clientState
	for _, email := range emails {
		port, _ := getFreePort()
		cState, err := startClient(clientBin, opts.root, email, serverURL, port)
		if err != nil {
			t.Fatalf("start client %s: %v", email, err)
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
		t.Fatalf("write state: %v", err)
	}

	// Wait for initial sync
	time.Sleep(2 * time.Second)

	// Create client helpers
	aliceHelper := &ClientHelper{
		t:         t,
		email:     emails[0],
		state:     clients[0],
		dataDir:   clients[0].DataPath,
		publicDir: filepath.Join(opts.root, emails[0], "datasites", emails[0], "public"),
		metrics:   &ClientMetrics{},
	}

	bobHelper := &ClientHelper{
		t:         t,
		email:     emails[1],
		state:     clients[1],
		dataDir:   clients[1].DataPath,
		publicDir: filepath.Join(opts.root, emails[1], "datasites", emails[1], "public"),
		metrics:   &ClientMetrics{},
	}

	metrics := &MetricsCollector{
		startTime:     time.Now(),
		customMetrics: make(map[string][]float64),
	}

	harness := &DevstackTestHarness{
		t:       t,
		root:    opts.root,
		state:   &state,
		alice:   aliceHelper,
		bob:     bobHelper,
		metrics: metrics,
		cleanup: []func(){
			func() { _ = killProcess(sState.PID) },
			func() { stopMinio(mState) },
			func() { _ = killProcess(clients[0].PID) },
			func() { _ = killProcess(clients[1].PID) },
		},
	}

	// Always cleanup processes, but preserve files if using sandbox
	t.Cleanup(func() {
		// Stop profiling first
		if err := harness.StopProfiling(); err != nil {
			t.Logf("Warning: failed to stop profiling: %v", err)
		}

		if os.Getenv("PERF_TEST_SANDBOX") != "" {
			t.Logf("Stopping processes but preserving sandbox at: %s", opts.root)
		}
		harness.Cleanup()
	})

	t.Logf("Devstack ready: server=%s, alice=pid:%d, bob=pid:%d",
		serverURL, clients[0].PID, clients[1].PID)

	return harness
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

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			if _, err := os.Stat(fullPath); err == nil {
				// File exists, verify content if MD5 provided
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

				c.t.Logf("%s received %s from %s", c.email, relPath, senderEmail)
				return nil
			}
		}
	}

	return fmt.Errorf("timeout waiting for file %s", fullPath)
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

	// Create ACL file
	aclContent := `rules:
  - pattern: '**/*.request'
    access:
      admin: []
      read:
        - '*'
      write:
        - '*'
  - pattern: '**/*.response'
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

	// Bob receives at: {bob's datasite}/datasites/{alice's datasite}/app_data/{appname}/rpc/{endpoint}/{filename}
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
