// Package utils provides utility functions and types for the SyftBox daemon.
package utils

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"sync/atomic"
)

// NumberedWriter implements io.Writer and adds line numbers to each line of output
type NumberedWriter struct {
	w       io.Writer
	counter *atomic.Uint64
	buf     *bytes.Buffer
	reader  *bufio.Reader
}

// NewNumberedWriter creates a new NumberedWriter that adds line numbers to each line
func NewNumberedWriter(w io.Writer) *NumberedWriter {
	buf := &bytes.Buffer{}
	return &NumberedWriter{
		w:       w,
		counter: &atomic.Uint64{},
		buf:     buf,
		reader:  bufio.NewReader(buf),
	}
}

// Write implements io.Writer
func (w *NumberedWriter) Write(p []byte) (n int, err error) {
	// Write to buffer
	n, err = w.buf.Write(p)
	if err != nil {
		return n, err
	}

	// Process any complete lines in the buffer
	for {
		line, err := w.reader.ReadBytes('\n')
		if err == io.EOF {
			// No complete line, put the bytes back
			w.buf.Write(line)
			break
		}
		if err != nil {
			return n, err
		}

		// Get the next line number
		lineNum := w.counter.Add(1)

		// Write the line number and the line
		_, err = io.WriteString(w.w, slog.Uint64("line", lineNum).String()+" ")
		if err != nil {
			return n, err
		}
		_, err = w.w.Write(line)
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

// Close flushes any remaining buffered data
func (w *NumberedWriter) Close() error {
	// Read any remaining data
	remaining, err := io.ReadAll(w.reader)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		// Get the next line number
		lineNum := w.counter.Add(1)

		// Write the line number and remaining data
		_, err = io.WriteString(w.w, slog.Uint64("line", lineNum).String()+" ")
		if err != nil {
			return err
		}
		_, err = w.w.Write(remaining)
		if err != nil {
			return err
		}
	}
	return nil
}
