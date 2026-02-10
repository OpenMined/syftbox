//go:build integration
// +build integration

package main

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type hotlinkLatencyRunConfig struct {
	name               string
	hotlink            bool
	ipcMode            string
	tcpAddr            string
	trace              bool
	priorityDebounceMs string
}

type hotlinkLatencyRunResult struct {
	name   string
	report *TestReport
	trace  *traceSummary
}

type traceSummary struct {
	metrics map[string]statSummary
}

type statSummary struct {
	Count int
	P50   float64
	P90   float64
	P95   float64
	P99   float64
}

func TestHotlinkLatencyCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	runs := []hotlinkLatencyRunConfig{
		{
			name:               "file",
			hotlink:            false,
			trace:              true,
			priorityDebounceMs: "0",
		},
		{
			name:               "hotlink-socket",
			hotlink:            true,
			trace:              true,
			priorityDebounceMs: "0",
		},
		{
			name:               "hotlink-tcp",
			hotlink:            true,
			ipcMode:            "tcp",
			tcpAddr:            "127.0.0.1:0",
			trace:              true,
			priorityDebounceMs: "0",
		},
	}

	results := make([]hotlinkLatencyRunResult, 0, len(runs))
	for _, run := range runs {
		results = append(results, runHotlinkLatencyE2EWithOptions(t, run))
	}

	logHotlinkLatencyComparison(t, results)
}

func runHotlinkLatencyE2EWithOptions(t *testing.T, cfg hotlinkLatencyRunConfig) hotlinkLatencyRunResult {
	t.Helper()

	if cfg.trace {
		t.Setenv("SYFTBOX_LATENCY_TRACE", "1")
	}

	if cfg.hotlink {
		t.Setenv("SYFTBOX_HOTLINK", "1")
		t.Setenv("SYFTBOX_HOTLINK_SOCKET_ONLY", "1")
	} else {
		t.Setenv("SYFTBOX_HOTLINK", "0")
		t.Setenv("SYFTBOX_HOTLINK_SOCKET_ONLY", "0")
	}

	if cfg.ipcMode != "" {
		t.Setenv("SYFTBOX_HOTLINK_IPC", cfg.ipcMode)
	} else {
		t.Setenv("SYFTBOX_HOTLINK_IPC", "")
	}

	if cfg.tcpAddr != "" {
		t.Setenv("SYFTBOX_HOTLINK_TCP_ADDR", cfg.tcpAddr)
	} else {
		t.Setenv("SYFTBOX_HOTLINK_TCP_ADDR", "")
	}

	if cfg.priorityDebounceMs != "" {
		t.Setenv("SYFTBOX_PRIORITY_DEBOUNCE_MS", cfg.priorityDebounceMs)
	}

	h := NewDevstackHarness(t)
	report := runHotlinkLatencyE2EWithHarness(t, h, cfg.hotlink, "TestHotlinkLatencyCompare-"+cfg.name)

	var trace *traceSummary
	if cfg.trace {
		trace = collectTraceSummary(t, h, cfg)
		logTraceSummary(t, cfg.name, trace)
	}

	return hotlinkLatencyRunResult{
		name:   cfg.name,
		report: report,
		trace:  trace,
	}
}

func collectTraceSummary(t *testing.T, h *DevstackTestHarness, cfg hotlinkLatencyRunConfig) *traceSummary {
	t.Helper()

	paths := make([]string, 0, 3)
	if h.state != nil {
		if h.state.Server.LogPath != "" {
			paths = append(paths, h.state.Server.LogPath)
		}
		for _, c := range h.state.Clients {
			if c.LogPath != "" {
				paths = append(paths, c.LogPath)
			}
		}
	}

	samples := collectLatencyTraceSamples(t, paths)
	return summarizeTraceSamples(samples, cfg)
}

func collectLatencyTraceSamples(t *testing.T, logPaths []string) map[string]map[string]float64 {
	t.Helper()

	out := make(map[string]map[string]float64)
	for _, path := range logPaths {
		if path == "" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
		for scanner.Scan() {
			fields := parseLogFields(scanner.Text())
			msg := fields["msg"]
			if msg == "" || !strings.HasPrefix(msg, "latency_trace ") {
				continue
			}
			stage := strings.TrimPrefix(msg, "latency_trace ")
			pathKey := tracePath(fields)
			if pathKey == "" {
				continue
			}
			value, ok := latencyTraceValue(stage, fields)
			if !ok {
				continue
			}
			entry := out[pathKey]
			if entry == nil {
				entry = make(map[string]float64)
				out[pathKey] = entry
			}
			entry[stage] = value
		}
		_ = f.Close()
	}

	return out
}

func parseLogFields(line string) map[string]string {
	fields := make(map[string]string)
	idx := 0
	for idx < len(line) {
		for idx < len(line) && line[idx] == ' ' {
			idx++
		}
		if idx >= len(line) {
			break
		}
		keyStart := idx
		for idx < len(line) && line[idx] != '=' {
			idx++
		}
		if idx >= len(line) {
			break
		}
		key := line[keyStart:idx]
		idx++
		if idx >= len(line) {
			fields[key] = ""
			break
		}
		var value string
		if line[idx] == '"' || line[idx] == '\'' {
			quote := line[idx]
			idx++
			valStart := idx
			for idx < len(line) && line[idx] != quote {
				idx++
			}
			value = line[valStart:idx]
			if idx < len(line) {
				idx++
			}
		} else {
			valStart := idx
			for idx < len(line) && line[idx] != ' ' {
				idx++
			}
			value = line[valStart:idx]
		}
		fields[key] = value
	}
	return fields
}

func tracePath(fields map[string]string) string {
	if path := strings.TrimSpace(fields["path"]); path != "" {
		return path
	}
	if path := strings.TrimSpace(fields["wsmsg.path"]); path != "" {
		return path
	}
	if path := strings.TrimSpace(fields["hotlink.path"]); path != "" {
		return path
	}
	return ""
}

func latencyTraceValue(stage string, fields map[string]string) (float64, bool) {
	switch stage {
	case "server_uploaded":
		return parseFloat(fields["upload_ms"])
	case "priority_upload_file":
		return parseFloat(fields["mod_age_ms"])
	case "watcher_detected":
		return 0, false
	default:
		return parseFloat(fields["age_ms"])
	}
}

func parseFloat(value string) (float64, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func summarizeTraceSamples(samples map[string]map[string]float64, cfg hotlinkLatencyRunConfig) *traceSummary {
	metrics := make(map[string][]float64)

	for _, sample := range samples {
		if cfg.hotlink {
			ipcRead, okRead := sample["hotlink_ipc_read"]
			serverRecv, okServer := sample["hotlink_server_received"]
			ipcWritten, okWrite := sample["hotlink_ipc_written"]
			if okRead {
				metrics["sender_ipc_read_ms"] = append(metrics["sender_ipc_read_ms"], ipcRead)
			}
			if okServer {
				metrics["server_recv_ms"] = append(metrics["server_recv_ms"], serverRecv)
			}
			if okWrite {
				metrics["receiver_ipc_write_ms"] = append(metrics["receiver_ipc_write_ms"], ipcWritten)
				metrics["end_to_end_ms"] = append(metrics["end_to_end_ms"], ipcWritten)
			}
			if okRead && okServer {
				metrics["client_to_server_ms"] = append(metrics["client_to_server_ms"], serverRecv-ipcRead)
			}
			if okServer && okWrite {
				metrics["server_to_receiver_ms"] = append(metrics["server_to_receiver_ms"], ipcWritten-serverRecv)
			}
			continue
		}

		senderRead, okRead := sample["priority_upload_read"]
		serverRecv, okServer := sample["server_received"]
		receiverWrite, okWrite := sample["priority_download_written"]
		if okRead {
			metrics["sender_read_ms"] = append(metrics["sender_read_ms"], senderRead)
		}
		if okServer {
			metrics["server_recv_ms"] = append(metrics["server_recv_ms"], serverRecv)
		}
		if okWrite {
			metrics["receiver_write_ms"] = append(metrics["receiver_write_ms"], receiverWrite)
			metrics["end_to_end_ms"] = append(metrics["end_to_end_ms"], receiverWrite)
		}
		if okRead && okServer {
			metrics["client_to_server_ms"] = append(metrics["client_to_server_ms"], serverRecv-senderRead)
		}
		if okServer && okWrite {
			metrics["server_to_receiver_ms"] = append(metrics["server_to_receiver_ms"], receiverWrite-serverRecv)
		}
		if ack, ok := sample["priority_upload_ack"]; ok {
			metrics["sender_ack_ms"] = append(metrics["sender_ack_ms"], ack)
		}
		if upload, ok := sample["server_uploaded"]; ok {
			metrics["server_upload_ms"] = append(metrics["server_upload_ms"], upload)
		}
		if modAge, ok := sample["priority_upload_file"]; ok {
			metrics["file_mod_age_ms"] = append(metrics["file_mod_age_ms"], modAge)
		}
	}

	summary := make(map[string]statSummary)
	for name, values := range metrics {
		summary[name] = summarize(values)
	}

	return &traceSummary{metrics: summary}
}

func summarize(values []float64) statSummary {
	if len(values) == 0 {
		return statSummary{}
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	return statSummary{
		Count: len(sorted),
		P50:   percentileFloat(sorted, 0.50),
		P90:   percentileFloat(sorted, 0.90),
		P95:   percentileFloat(sorted, 0.95),
		P99:   percentileFloat(sorted, 0.99),
	}
}

func percentileFloat(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func logTraceSummary(t *testing.T, name string, summary *traceSummary) {
	t.Helper()
	if summary == nil || len(summary.metrics) == 0 {
		t.Logf("Trace summary (%s): no samples", name)
		return
	}

	keys := make([]string, 0, len(summary.metrics))
	for k := range summary.metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	t.Logf("=== Trace Breakdown (%s) ===", name)
	for _, key := range keys {
		stat := summary.metrics[key]
		t.Logf("%s: n=%d p50=%.3fms p90=%.3fms p95=%.3fms p99=%.3fms", key, stat.Count, stat.P50, stat.P90, stat.P95, stat.P99)
	}
	t.Logf("=============================")
}

func logHotlinkLatencyComparison(t *testing.T, results []hotlinkLatencyRunResult) {
	t.Helper()
	if len(results) == 0 {
		return
	}

	t.Logf("=== Hotlink Latency Comparison ===")
	for _, res := range results {
		if res.report == nil {
			continue
		}
		t.Logf("%s: p50=%s p90=%s p95=%s p99=%s", res.name, res.report.P50Latency, res.report.P90Latency, res.report.P95Latency, res.report.P99Latency)
	}
	t.Logf("==================================")
}
