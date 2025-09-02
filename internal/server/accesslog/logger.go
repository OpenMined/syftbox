package accesslog

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
)

type AccessLogger struct {
	baseDir     string
	writers     map[string]*userLogWriter
	writerMutex sync.RWMutex
	logger      *slog.Logger
}

func New(baseDir string, logger *slog.Logger) (*AccessLogger, error) {
	if err := os.MkdirAll(baseDir, LogDirPermission); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	return &AccessLogger{
		baseDir: baseDir,
		writers: make(map[string]*userLogWriter),
		logger:  logger.With("component", "access_logger"),
	}, nil
}

// LogAccess logs an access attempt
// It creates a new access log entry and writes it to the log file

func (al *AccessLogger) LogAccess(ctx *gin.Context, path string, accessType AccessType, accessLevel acl.AccessLevel, allowed bool, deniedReason string) {
	user := ctx.GetString("user")
	if user == "" {
		user = "anonymous"
	}

	entry := AccessLogEntry{
		Timestamp:    time.Now().UTC(),
		Path:         path,
		AccessType:   accessType,
		User:         user,
		IP:           ctx.ClientIP(),
		UserAgent:    ctx.Request.UserAgent(),
		Method:       ctx.Request.Method,
		StatusCode:   ctx.Writer.Status(),
		Allowed:      allowed,
		DeniedReason: deniedReason,
	}

	if err := al.writeLog(user, entry); err != nil {
		al.logger.Error("failed to write access log",
			"user", user,
			"error", err,
			"path", path)
	}
}

// writeLog writes a log entry to the log file
// It creates a new user writer if it doesn't exist
// It also writes the log entry to the log file
func (al *AccessLogger) writeLog(user string, entry AccessLogEntry) error {
	al.writerMutex.Lock()
	writer, exists := al.writers[user]
	if !exists {
		var err error
		writer, err = al.createUserWriter(user)
		if err != nil {
			al.writerMutex.Unlock()
			return err
		}
		al.writers[user] = writer
	}
	al.writerMutex.Unlock()

	return writer.writeEntry(entry)
}

// createUserWriter creates a user writer
// It creates a new user writer if it doesn't exist
// It also creates a new log file if it doesn't exist
func (al *AccessLogger) createUserWriter(user string) (*userLogWriter, error) {
	userDir := filepath.Join(al.baseDir, sanitizeUsername(user))
	if err := os.MkdirAll(userDir, LogDirPermission); err != nil {
		return nil, fmt.Errorf("failed to create user log directory: %w", err)
	}

	writer := &userLogWriter{
		user:   user,
		logDir: userDir,
	}

	if err := writer.openLogFile(); err != nil {
		return nil, err
	}

	return writer, nil
}

func (al *AccessLogger) Close() error {
	al.writerMutex.Lock()
	defer al.writerMutex.Unlock()

	for _, writer := range al.writers {
		if err := writer.close(); err != nil {
			al.logger.Error("failed to close writer", "user", writer.user, "error", err)
		}
	}

	return nil
}

// GetUserLogs gets the logs for a user
// It reads the log files for the user and returns the logs
// It also limits the number of logs returned
func (al *AccessLogger) GetUserLogs(user string, limit int) ([]AccessLogEntry, error) {
	userDir := filepath.Join(al.baseDir, sanitizeUsername(user))

	files, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []AccessLogEntry{}, nil
		}
		return nil, err
	}

	var entries []AccessLogEntry
	for i := len(files) - 1; i >= 0 && len(entries) < limit; i-- {
		if files[i].IsDir() || filepath.Ext(files[i].Name()) != ".log" {
			continue
		}

		logPath := filepath.Join(userDir, files[i].Name())
		fileEntries, err := al.readLogFile(logPath, limit-len(entries))
		if err != nil {
			al.logger.Warn("failed to read log file", "file", logPath, "error", err)
			continue
		}

		entries = append(fileEntries, entries...)
	}

	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	return entries, nil
}

func (al *AccessLogger) readLogFile(path string, limit int) ([]AccessLogEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []AccessLogEntry
	decoder := json.NewDecoder(file)

	for {
		var entry AccessLogEntry
		if err := decoder.Decode(&entry); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}
		entries = append(entries, entry)
	}

	if len(entries) > limit {
		return entries[len(entries)-limit:], nil
	}

	return entries, nil
}
