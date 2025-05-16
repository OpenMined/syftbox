package utils

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"sync/atomic"
	"time"
)

const (
	// maxBufferSize is the maximum size of a single line that will be buffered
	maxBufferSize = 1024 * 1024 // 1MB
)

// LogInterceptor implements io.Writer and intercepts output to add structured logging information.
// It adds a sequence number and timestamp to each line of output.
type LogInterceptor struct {
	target          io.Writer
	sequenceNumber  *atomic.Uint64
	interceptBuf    *bytes.Buffer
	interceptReader *bufio.Reader
}

// NewLogInterceptor creates a new LogInterceptor that adds structured logging information to each line.
// The interceptor will write to the provided target writer, adding a sequence number and timestamp
// to each line of output.
func NewLogInterceptor(target io.Writer) *LogInterceptor {
	buf := &bytes.Buffer{}
	return &LogInterceptor{
		target:          target,
		sequenceNumber:  &atomic.Uint64{},
		interceptBuf:    buf,
		interceptReader: bufio.NewReader(buf),
	}
}

// writeFormattedLine writes a line with sequence number and timestamp to the target writer.
// Returns the total number of bytes written and any error encountered.
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

	// Write the line
	n, err = i.target.Write(line)
	totalWritten += n
	return totalWritten, err
}

// Write implements io.Writer. It buffers the input and processes complete lines,
// adding sequence numbers and timestamps to each line.
// Returns the number of bytes from p that were processed and any error encountered.
func (i *LogInterceptor) Write(p []byte) (n int, err error) {
	// Write to buffer
	bytesWritten, err := i.interceptBuf.Write(p)
	if err != nil {
		return 0, err
	}

	bytesProcessed := 0
	// Process any complete lines in the buffer
	for {
		line, err := i.interceptReader.ReadBytes('\n')
		if err == io.EOF {
			// No complete line, put the bytes back if they're not too large
			if len(line) > maxBufferSize {
				return bytesProcessed, io.ErrShortBuffer
			}
			i.interceptBuf.Write(line)
			break
		}
		if err != nil {
			return bytesProcessed, err
		}

		// Write the formatted line to target
		_, err = i.writeFormattedLine(line)
		if err != nil {
			return bytesProcessed, err
		}

		// Update bytes processed - this is the actual input bytes we've handled
		bytesProcessed += len(line)
	}

	return bytesWritten, nil
}

// Close flushes any remaining buffered data to the target writer.
// Returns any error encountered during the flush operation.
func (i *LogInterceptor) Close() error {
	// Read any remaining data
	remaining, err := io.ReadAll(i.interceptReader)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		_, err = i.writeFormattedLine(remaining)
	}
	return err
}
