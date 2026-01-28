//go:build integration
// +build integration

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	hotlinkFrameMagic   = "HLNK"
	hotlinkFrameVersion = 1
	hotlinkMaxPathLen   = 4096
	hotlinkMaxETagLen   = 128
	hotlinkMaxPayload   = 16 * 1024 * 1024
)

// TestHotlinkLatencyE2E measures end-to-end latency for priority RPC request files.
// This is a focused latency benchmark (smaller + more iterations) to act as a baseline
// before hotlink optimizations land.
func TestHotlinkLatencyE2E(t *testing.T) {
	_ = runHotlinkLatencyE2E(t, false)
}

// TestHotlinkLatencyE2EHotlink runs the same benchmark with hotlink enabled.
func TestHotlinkLatencyE2EHotlink(t *testing.T) {
	t.Setenv("SYFTBOX_HOTLINK", "1")
	t.Setenv("SYFTBOX_HOTLINK_SOCKET_ONLY", "1")
	t.Setenv("SYFTBOX_PRIORITY_DEBOUNCE_MS", "0")
	_ = runHotlinkLatencyE2E(t, true)
}

func runHotlinkLatencyE2E(t *testing.T, hotlink bool) *TestReport {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	h := NewDevstackHarness(t)
	return runHotlinkLatencyE2EWithHarness(t, h, hotlink, "TestHotlinkLatencyE2E")
}

func runHotlinkLatencyE2EWithHarness(t *testing.T, h *DevstackTestHarness, hotlink bool, testName string) *TestReport {
	if err := h.StartProfiling(testName); err != nil {
		t.Fatalf("start profiling: %v", err)
	}
	defer h.StopProfiling()

	// Create default ACLs (required for Bob to see Alice's public files)
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}
	if err := h.AllowSubscriptionsBetween(h.alice, h.bob); err != nil {
		t.Fatalf("set subscriptions: %v", err)
	}

	appName := "latency"
	endpoint := "hotlink"

	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}
	if hotlink {
		acceptDir := filepath.Dir(h.bob.rpcReceivePath(h.alice.email, appName, endpoint, "accept.tmp"))
		if err := os.MkdirAll(acceptDir, 0o755); err != nil {
			t.Fatalf("create hotlink dir: %v", err)
		}
		acceptPath := filepath.Join(acceptDir, "stream.accept")
		if err := os.WriteFile(acceptPath, []byte("1"), 0o644); err != nil {
			t.Fatalf("write hotlink accept file: %v", err)
		}
		senderMarker := hotlinkSenderIPCPath(h, appName, endpoint)
		if err := os.MkdirAll(filepath.Dir(senderMarker), 0o755); err != nil {
			t.Fatalf("create hotlink sender dir: %v", err)
		}
		if err := os.WriteFile(senderMarker, []byte(""), 0o644); err != nil {
			t.Fatalf("create hotlink sender marker: %v", err)
		}
	}

	// Allow initial WS + ACL propagation to settle.
	time.Sleep(1 * time.Second)

	// Warmup
	warmupPayload := timedPayload(64)
	warmupHash := CalculateMD5(warmupPayload)
	var (
		conn    net.Conn
		frameCh <-chan *hotlinkFrame
		errCh   <-chan error
	)
	var senderConn net.Conn
	var connErr error
	if hotlink {
		senderConn, connErr = dialHotlinkIPC(hotlinkSenderIPCPath(h, appName, endpoint), 10*time.Second)
		if senderConn == nil {
			t.Fatalf("open sender ipc: %v", connErr)
		}
		defer senderConn.Close()
		if err := writeHotlinkFrame(senderConn, hotlinkRelPath(h.alice.email, appName, endpoint, "warmup.request"), warmupPayload); err != nil {
			t.Fatalf("warmup send: %v", err)
		}
		conn, connErr = dialHotlinkIPC(hotlinkIPCPath(h, appName, endpoint), 10*time.Second)
		if connErr != nil {
			t.Fatalf("open ipc: %v", connErr)
		}
		defer conn.Close()
		frameCh, errCh = startHotlinkFrameReader(conn)
		if _, err := readHotlinkPayload(frameCh, errCh, "", 0, 10*time.Second); err != nil {
			t.Fatalf("warmup ipc read: %v", err)
		}
	} else {
		if err := h.alice.UploadRPCRequest(appName, endpoint, "warmup.request", warmupPayload); err != nil {
			t.Fatalf("warmup upload: %v", err)
		}
		if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, "warmup.request", warmupHash, 5*time.Second); err != nil {
			t.Fatalf("warmup wait: %v", err)
		}
	}

	testCases := []struct {
		name       string
		size       int
		iterations int
		timeout    time.Duration
	}{
		{"1KB", 1024, 30, 2 * time.Second},
		{"10KB", 10 * 1024, 30, 2 * time.Second},
		{"100KB", 100 * 1024, 20, 3 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < tc.iterations; i++ {
				filename := fmt.Sprintf("lat-%s-%03d.request", tc.name, i)
				payload := timedPayload(tc.size)

				start := time.Now()
				if hotlink && senderConn != nil {
					if err := writeHotlinkFrame(senderConn, hotlinkRelPath(h.alice.email, appName, endpoint, filename), payload); err != nil {
						t.Fatalf("hotlink send failed: %v", err)
					}
				} else {
					if err := h.alice.UploadRPCRequest(appName, endpoint, filename, payload); err != nil {
						t.Fatalf("upload failed: %v", err)
					}
				}

				var (
					latency time.Duration
					err     error
				)
				if hotlink {
					expectedPath := hotlinkRelPath(h.alice.email, appName, endpoint, filename)
					latency, err = readHotlinkPayloadWithTimestamp(frameCh, errCh, expectedPath, tc.size, tc.timeout)
				} else {
					latency, err = waitForRPCRequestWithTimestamp(
						h.bob,
						h.alice.email,
						appName,
						endpoint,
						filename,
						tc.timeout,
					)
				}
				if err != nil {
					t.Fatalf("wait failed: %v", err)
				}

				// end-to-end time from write to receive (same-host monotonic)
				total := time.Since(start)
				h.metrics.RecordLatency(total)
				h.metrics.RecordCustomMetric("payload_to_read_latency_ms", float64(latency.Milliseconds()))
			}
		})
	}

	report := h.metrics.GenerateReport()
	report.Log(t)
	return report
}

func timedPayload(size int) []byte {
	if size < 8 {
		size = 8
	}
	payload := GenerateRandomFile(size)
	binary.LittleEndian.PutUint64(payload[:8], uint64(time.Now().UnixNano()))
	return payload
}

func waitForRPCRequestWithTimestamp(
	c *ClientHelper,
	senderEmail, appName, endpoint, filename string,
	timeout time.Duration,
) (time.Duration, error) {
	c.t.Helper()

	fullPath := c.rpcReceivePath(senderEmail, appName, endpoint, filename)
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		content, err := c.readRPCPayload(fullPath)
		if err != nil {
			continue
		}
		if len(content) < 8 {
			return 0, fmt.Errorf("payload too small for timestamp")
		}
		sentNs := int64(binary.LittleEndian.Uint64(content[:8]))
		return time.Since(time.Unix(0, sentNs)), nil
	}

	return 0, fmt.Errorf("timeout waiting for RPC request: %s", fullPath)
}

func hotlinkIPCPath(h *DevstackTestHarness, appName, endpoint string) string {
	name := hotlinkIPCMarkerName()
	return filepath.Join(
		h.bob.dataDir,
		"datasites",
		h.alice.email,
		"app_data",
		appName,
		"rpc",
		endpoint,
		name,
	)
}

func hotlinkSenderIPCPath(h *DevstackTestHarness, appName, endpoint string) string {
	name := hotlinkIPCMarkerName()
	return filepath.Join(
		h.alice.dataDir,
		"datasites",
		h.alice.email,
		"app_data",
		appName,
		"rpc",
		endpoint,
		name,
	)
}

func hotlinkRelPath(senderEmail, appName, endpoint, filename string) string {
	return filepath.Join(
		senderEmail,
		"app_data",
		appName,
		"rpc",
		endpoint,
		filename,
	)
}

func hotlinkIPCMarkerName() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SYFTBOX_HOTLINK_IPC")))
	if mode == "tcp" {
		return "stream.tcp"
	}
	if runtime.GOOS == "windows" {
		return "stream.pipe"
	}
	return "stream.sock"
}

func dialHotlinkIPC(path string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			target := ""
			if data, err := os.ReadFile(path); err == nil {
				target = strings.TrimSpace(string(data))
			}
			if target == "" {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if strings.HasPrefix(target, "tcp://") {
				target = strings.TrimPrefix(target, "tcp://")
			}
			if strings.Contains(target, ":") && !strings.HasPrefix(target, "/") {
				if conn, err := net.Dial("tcp", target); err == nil {
					return conn, nil
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if runtime.GOOS != "windows" {
				if conn, err := net.Dial("unix", target); err == nil {
					return conn, nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("timeout waiting for ipc")
}

func readHotlinkPayload(frameCh <-chan *hotlinkFrame, errCh <-chan error, expectedPath string, size int, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frame, err := nextHotlinkFrame(frameCh, errCh, time.Until(deadline))
		if err != nil {
			return nil, err
		}
		if expectedPath != "" && frame.path != expectedPath {
			continue
		}
		if size > 0 && len(frame.payload) != size {
			continue
		}
		return frame.payload, nil
	}
	return nil, fmt.Errorf("timeout waiting for hotlink frame")
}

func readHotlinkPayloadWithTimestamp(frameCh <-chan *hotlinkFrame, errCh <-chan error, expectedPath string, size int, timeout time.Duration) (time.Duration, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frame, err := nextHotlinkFrame(frameCh, errCh, time.Until(deadline))
		if err != nil {
			return 0, fmt.Errorf("%w (expectedPath=%q, size=%d)", err, expectedPath, size)
		}
		if expectedPath != "" && frame.path != expectedPath {
			fmt.Printf("DEBUG: path mismatch: got %q, want %q\n", frame.path, expectedPath)
			continue
		}
		if size > 0 && len(frame.payload) != size {
			fmt.Printf("DEBUG: size mismatch: got %d, want %d\n", len(frame.payload), size)
			continue
		}
		if len(frame.payload) < 8 {
			return 0, fmt.Errorf("payload too small for timestamp")
		}
		sentNs := int64(binary.LittleEndian.Uint64(frame.payload[:8]))
		return time.Since(time.Unix(0, sentNs)), nil
	}
	return 0, fmt.Errorf("timeout waiting for hotlink frame")
}

func writeHotlinkFrame(w io.Writer, path string, payload []byte) error {
	frame := encodeHotlinkFrame(path, "", 0, payload)
	_, err := w.Write(frame)
	return err
}

func encodeHotlinkFrame(path, etag string, seq uint64, payload []byte) []byte {
	pathBytes := []byte(path)
	etagBytes := []byte(etag)
	headerLen := 4 + 1 + 2 + 2 + 4 + 8
	total := headerLen + len(pathBytes) + len(etagBytes) + len(payload)
	buf := bytes.NewBuffer(make([]byte, 0, total))
	buf.WriteString(hotlinkFrameMagic)
	buf.WriteByte(byte(hotlinkFrameVersion))
	_ = binary.Write(buf, binary.BigEndian, uint16(len(pathBytes)))
	_ = binary.Write(buf, binary.BigEndian, uint16(len(etagBytes)))
	_ = binary.Write(buf, binary.BigEndian, uint32(len(payload)))
	_ = binary.Write(buf, binary.BigEndian, seq)
	buf.Write(pathBytes)
	buf.Write(etagBytes)
	buf.Write(payload)
	return buf.Bytes()
}

type hotlinkFrame struct {
	path    string
	etag    string
	seq     uint64
	payload []byte
}

func startHotlinkFrameReader(r io.Reader) (<-chan *hotlinkFrame, <-chan error) {
	frameCh := make(chan *hotlinkFrame, 8)
	errCh := make(chan error, 1)
	go func() {
		defer close(frameCh)
		reader := bufio.NewReader(r)
		for {
			frame, err := readHotlinkFrameBlocking(reader)
			if err != nil {
				errCh <- err
				return
			}
			frameCh <- frame
		}
	}()
	return frameCh, errCh
}

func nextHotlinkFrame(frameCh <-chan *hotlinkFrame, errCh <-chan error, timeout time.Duration) (*hotlinkFrame, error) {
	select {
	case frame, ok := <-frameCh:
		if !ok {
			select {
			case err := <-errCh:
				return nil, err
			default:
				return nil, fmt.Errorf("hotlink frame channel closed")
			}
		}
		fmt.Printf("DEBUG: nextHotlinkFrame received: path=%q, payloadLen=%d\n", frame.path, len(frame.payload))
		return frame, nil
	case err := <-errCh:
		return nil, fmt.Errorf("frame reader error: %w", err)
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for hotlink frame")
	}
}

func readHotlinkFrameBlocking(r *bufio.Reader) (*hotlinkFrame, error) {
	if r == nil {
		return nil, fmt.Errorf("fifo not open")
	}

	magic := []byte(hotlinkFrameMagic)
	window := make([]byte, 0, len(magic))

	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		window = append(window, b)
		if len(window) > len(magic) {
			window = window[1:]
		}
		if len(window) < len(magic) || !bytes.Equal(window, magic) {
			continue
		}

		header := make([]byte, 1+2+2+4+8)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, err
		}
		if header[0] != hotlinkFrameVersion {
			window = window[:0]
			continue
		}
		pathLen := binary.BigEndian.Uint16(header[1:3])
		etagLen := binary.BigEndian.Uint16(header[3:5])
		payloadLen := binary.BigEndian.Uint32(header[5:9])
		seq := binary.BigEndian.Uint64(header[9:17])

		if pathLen > hotlinkMaxPathLen || etagLen > hotlinkMaxETagLen || payloadLen > hotlinkMaxPayload {
			window = window[:0]
			continue
		}

		frame := &hotlinkFrame{seq: seq}
		if pathLen > 0 {
			path := make([]byte, pathLen)
			if _, err := io.ReadFull(r, path); err != nil {
				return nil, err
			}
			frame.path = string(path)
		}
		if etagLen > 0 {
			etag := make([]byte, etagLen)
			if _, err := io.ReadFull(r, etag); err != nil {
				return nil, err
			}
			frame.etag = string(etag)
		}
		if payloadLen > 0 {
			frame.payload = make([]byte, payloadLen)
			if _, err := io.ReadFull(r, frame.payload); err != nil {
				return nil, err
			}
		}
		return frame, nil
	}
}
