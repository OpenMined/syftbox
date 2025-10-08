package accesslog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type userLogWriter struct {
	user        string
	file        *os.File
	currentSize int64
	mutex       sync.Mutex
	logDir      string
	currentFile string
}

// writeEntry writes a log entry to the log file
// It marshals the log entry to JSON and writes it to the file
// It also checks if the log file is too large and rotates it if needed
func (w *userLogWriter) writeEntry(entry AccessLogEntry) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	data = append(data, '\n')

	// Check if the log file is too large
	// If so, rotate the log file
	if w.currentSize+int64(len(data)) > MaxLogSize {
		if err := w.rotate(); err != nil {
			return fmt.Errorf("failed to rotate log: %w", err)
		}
	}

	// Write the log entry to the file
	n, err := w.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write log entry: %w", err)
	}

	w.currentSize += int64(n)
	return nil
}

// openLogFile opens the log file
// It creates a new log file if it doesn't exist
// It also sets the file and current size
func (w *userLogWriter) openLogFile() error {
	filename := fmt.Sprintf("access_%s.log", time.Now().Format("20060102"))
	logPath := filepath.Join(w.logDir, filename)

	// Open the log file
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, LogFilePermission)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Get the file size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	// Set the file and current size
	w.file = file
	w.currentSize = stat.Size()
	w.currentFile = logPath

	return nil
}

// rotate rotates the log file
// It closes the current log file and reopens a new one
// It also cleans up old logs
func (w *userLogWriter) rotate() error {
	if w.file != nil {
		w.file.Close()
	}

	timestamp := time.Now().Format("20060102_150405")
	newName := fmt.Sprintf("access_%s.log", timestamp)
	newPath := filepath.Join(w.logDir, newName)

	if err := os.Rename(w.currentFile, newPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	if err := w.cleanOldLogs(); err != nil {
		return fmt.Errorf("failed to clean old logs: %w", err)
	}

	return w.openLogFile()
}

func (w *userLogWriter) cleanOldLogs() error {
	files, err := os.ReadDir(w.logDir)
	if err != nil {
		return err
	}

	var logFiles []os.DirEntry
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".log" {
			logFiles = append(logFiles, file)
		}
	}

	if len(logFiles) <= MaxLogFiles {
		return nil
	}

	toDelete := len(logFiles) - MaxLogFiles
	for i := 0; i < toDelete; i++ {
		oldFile := filepath.Join(w.logDir, logFiles[i].Name())
		if err := os.Remove(oldFile); err != nil {
			return fmt.Errorf("failed to remove old log file: %w", err)
		}
	}

	return nil
}

// close closes the log file
func (w *userLogWriter) close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
