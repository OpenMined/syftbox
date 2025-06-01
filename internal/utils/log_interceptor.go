package utils

import (
	"bytes"
	"io"
	"log/slog"
	"sync/atomic"
	"time"
)

const (
	// maxBufferSize is the maximum size of incomplete line data that will be buffered
	maxBufferSize = 1024 * 1024 // 1MB
)

// LogInterceptor implements io.Writer and intercepts output to add structured logging information.
// It adds a sequence number and timestamp to each line of output.
type LogInterceptor struct {
	target         io.WriteCloser
	sequenceNumber *atomic.Uint64
	buffer         *bytes.Buffer // buffer for incomplete lines
}

// NewLogInterceptor creates a new LogInterceptor that adds structured logging information to each line.
// The interceptor will write to the provided target writer, adding a sequence number and timestamp
// to each line of output.
func NewLogInterceptor(target io.WriteCloser) *LogInterceptor {
	return &LogInterceptor{
		target:         target,
		sequenceNumber: &atomic.Uint64{},
		buffer:         &bytes.Buffer{},
	}
}

// writeFormattedLine writes a line with sequence number and timestamp to the target writer.
// The line should include the newline character if desired.
// Returns the number of bytes written and any error encountered.
func (i *LogInterceptor) writeFormattedLine(line []byte) (int, error) {
	lineNum := i.sequenceNumber.Add(1)
	totalWritten := 0

	// Write the line number
	lineNumStr := slog.Uint64("line", lineNum).String() + " "
	n, err := io.WriteString(i.target, lineNumStr)
	totalWritten += n
	if err != nil {
		return totalWritten, err
	}

	// Write the timestamp
	timeStr := slog.String("time", time.Now().Format(time.RFC3339)).String() + " "
	n, err = io.WriteString(i.target, timeStr)
	totalWritten += n
	if err != nil {
		return totalWritten, err
	}

	// Write the actual line content
	n, err = i.target.Write(line)
	totalWritten += n
	return totalWritten, err
}

// Write implements io.Writer. It processes input data line by line,
// adding sequence numbers and timestamps to each complete line.
// Incomplete lines are buffered until a newline is received.
// Returns the number of bytes from p that were processed and any error encountered.
func (i *LogInterceptor) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Write new data to buffer
	_, err = i.buffer.Write(p)
	if err != nil {
		return 0, err
	}

	// Check buffer size limit
	if i.buffer.Len() > maxBufferSize {
		return 0, io.ErrShortBuffer
	}

	// Process complete lines from buffer
	data := i.buffer.Bytes()
	lastNewline := -1

	// Find all complete lines (ending with \n)
	for j := 0; j < len(data); j++ {
		if data[j] == '\n' {
			// Found a complete line, write it
			line := data[lastNewline+1 : j+1]
			_, err = i.writeFormattedLine(line)
			if err != nil {
				return 0, err
			}
			lastNewline = j
		}
	}

	// Remove processed lines from buffer, keep any incomplete line
	if lastNewline >= 0 {
		// We processed some complete lines, remove them from buffer
		remaining := data[lastNewline+1:]
		i.buffer.Reset()
		if len(remaining) > 0 {
			i.buffer.Write(remaining)
		}
	}

	// We successfully processed all input bytes
	return len(p), nil
}

// Close flushes any remaining buffered data to the target writer and closes it.
// If there's incomplete line data in the buffer, it will be written without a trailing newline.
// Returns any error encountered during the flush or close operation.
func (i *LogInterceptor) Close() error {
	// Write any remaining buffered data as a final line
	if i.buffer.Len() > 0 {
		_, err := i.writeFormattedLine(i.buffer.Bytes())
		if err != nil {
			return err
		}
		i.buffer.Reset()
	}

	// Close the target writer
	return i.target.Close()
}
