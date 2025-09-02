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

const (
	MaxLogSize        = 10 * 1024 * 1024 // 10MB
	MaxLogFiles       = 5
	LogFilePermission = 0600
	LogDirPermission  = 0700
)

type AccessType string

const (
	AccessTypeAdmin AccessType = "admin"
	AccessTypeRead  AccessType = "read"
	AccessTypeWrite AccessType = "write"
	AccessTypeDeny  AccessType = "deny"
)

type AccessLogEntry struct {
	Timestamp   time.Time  `json:"timestamp"`
	Path        string     `json:"path"`
	AccessType  AccessType `json:"access_type"`
	User        string     `json:"user"`
	IP          string     `json:"ip"`
	UserAgent   string     `json:"user_agent"`
	Method      string     `json:"method"`
	StatusCode  int        `json:"status_code"`
	Allowed     bool       `json:"allowed"`
	DeniedReason string    `json:"denied_reason,omitempty"`
}

// MarshalJSON customizes the JSON output for AccessLogEntry
func (e AccessLogEntry) MarshalJSON() ([]byte, error) {
	// Use an anonymous struct that mirrors our fields but with formatted timestamp
	return json.Marshal(&struct {
		Timestamp    string     `json:"timestamp"`
		Path         string     `json:"path"`
		AccessType   AccessType `json:"access_type"`
		User         string     `json:"user"`
		IP           string     `json:"ip"`
		UserAgent    string     `json:"user_agent"`
		Method       string     `json:"method"`
		StatusCode   int        `json:"status_code"`
		Allowed      bool       `json:"allowed"`
		DeniedReason string     `json:"denied_reason,omitempty"`
	}{
		Timestamp:    e.Timestamp.Format("2006-01-02 15:04:05.000 UTC"),
		Path:         e.Path,
		AccessType:   e.AccessType,
		User:         e.User,
		IP:           e.IP,
		UserAgent:    e.UserAgent,
		Method:       e.Method,
		StatusCode:   e.StatusCode,
		Allowed:      e.Allowed,
		DeniedReason: e.DeniedReason,
	})
}

// UnmarshalJSON customizes the JSON parsing for AccessLogEntry
func (e *AccessLogEntry) UnmarshalJSON(data []byte) error {
	// Use an anonymous struct that mirrors our fields but with string timestamp
	aux := &struct {
		Timestamp    string     `json:"timestamp"`
		Path         string     `json:"path"`
		AccessType   AccessType `json:"access_type"`
		User         string     `json:"user"`
		IP           string     `json:"ip"`
		UserAgent    string     `json:"user_agent"`
		Method       string     `json:"method"`
		StatusCode   int        `json:"status_code"`
		Allowed      bool       `json:"allowed"`
		DeniedReason string     `json:"denied_reason,omitempty"`
	}{}
	
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	
	// Parse the timestamp string back to time.Time
	t, err := time.Parse("2006-01-02 15:04:05.000 MST", aux.Timestamp)
	if err != nil {
		// Try to parse RFC3339 format for backward compatibility
		t, err = time.Parse(time.RFC3339, aux.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to parse timestamp: %w", err)
		}
	}
	
	e.Timestamp = t
	e.Path = aux.Path
	e.AccessType = aux.AccessType
	e.User = aux.User
	e.IP = aux.IP
	e.UserAgent = aux.UserAgent
	e.Method = aux.Method
	e.StatusCode = aux.StatusCode
	e.Allowed = aux.Allowed
	e.DeniedReason = aux.DeniedReason
	
	return nil
}

type AccessLogger struct {
	baseDir     string
	writers     map[string]*userLogWriter
	writerMutex sync.RWMutex
	logger      *slog.Logger
}

type userLogWriter struct {
	user        string
	file        *os.File
	currentSize int64
	mutex       sync.Mutex
	logDir      string
	currentFile string
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

func (w *userLogWriter) openLogFile() error {
	filename := fmt.Sprintf("access_%s.log", time.Now().Format("20060102"))
	logPath := filepath.Join(w.logDir, filename)
	
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, LogFilePermission)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	w.file = file
	w.currentSize = stat.Size()
	w.currentFile = logPath

	return nil
}

func (w *userLogWriter) writeEntry(entry AccessLogEntry) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	data = append(data, '\n')

	if w.currentSize+int64(len(data)) > MaxLogSize {
		if err := w.rotate(); err != nil {
			return fmt.Errorf("failed to rotate log: %w", err)
		}
	}

	n, err := w.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write log entry: %w", err)
	}

	w.currentSize += int64(n)
	return nil
}

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

func (al *AccessLogger) Close() error {
	al.writerMutex.Lock()
	defer al.writerMutex.Unlock()

	for _, writer := range al.writers {
		if writer.file != nil {
			writer.file.Close()
		}
	}

	return nil
}

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

func sanitizeUsername(user string) string {
	result := make([]byte, 0, len(user))
	for i := 0; i < len(user); i++ {
		c := user[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '@' || c == '.' || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

func ConvertACLLevel(level acl.AccessLevel) AccessType {
	switch level {
	case acl.AccessAdmin:
		return AccessTypeAdmin
	case acl.AccessWrite:
		return AccessTypeWrite
	case acl.AccessRead:
		return AccessTypeRead
	default:
		return AccessTypeDeny
	}
}