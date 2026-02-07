package sync

import (
	"encoding/binary"
	"os"
)

const latencyTraceEnv = "SYFTBOX_LATENCY_TRACE"

func latencyTraceEnabled() bool {
	return os.Getenv(latencyTraceEnv) == "1"
}

func payloadTimestampNs(payload []byte) (int64, bool) {
	if len(payload) < 8 {
		return 0, false
	}
	return int64(binary.LittleEndian.Uint64(payload[:8])), true
}
