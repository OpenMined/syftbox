package handlers

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

// LogsHandler handles log-related requests
type LogsHandler struct {
	mgr          *datasitemgr.DatasiteManager
	lineRegex    *regexp.Regexp
	timeRegex    *regexp.Regexp
	messageRegex *regexp.Regexp
}

// NewLogsHandler creates a new handler for logs
func NewLogsHandler(mgr *datasitemgr.DatasiteManager) *LogsHandler {
	return &LogsHandler{
		mgr:          mgr,
		lineRegex:    regexp.MustCompile(`line=(\d+)`),
		timeRegex:    regexp.MustCompile(`time=([^\s]+)`),
		messageRegex: regexp.MustCompile(`^(?:line=\d+\s+)?(?:time=[^\s]+\s+)?(.*)$`),
	}
}

func (h *LogsHandler) getLogFilePath(appId string) string {
	appId = strings.ToLower(appId)
	if appId == "" || appId == "system" {
		return config.DefaultLogFilePath
	}
	datasite, err := h.mgr.Get()
	if err != nil {
		return ""
	}
	appPath := filepath.Join(datasite.GetAppManager().AppsDir, appId)
	if apps.IsValidApp(appPath) {
		return filepath.Join(appPath, "logs", "app.log")
	}
	// Special case for manually installed local apps
	if suffix, found := strings.CutPrefix(appId, "local."); found {
		appPath := filepath.Join(datasite.GetAppManager().AppsDir, suffix)
		if apps.IsValidApp(appPath) {
			return filepath.Join(appPath, "logs", "app.log")
		}
	}
	return ""
}

// GetLogs handles GET requests to retrieve logs
//
//	@Summary		Get logs
//	@Description	Get system logs with pagination support
//	@Tags			Logs
//	@Produce		json
//	@Param			appId			query		string	false	"The ID of the app to retrieve logs for"										default(system)
//	@Param			startingToken	query		int		false	"Pagination token from a previous request to retrieve the next page of results"	default(1)		minimum(1)
//	@Param			maxResults		query		int		false	"Maximum number of lines to read"												default(100)	minimum(1)	maximum(1000)
//	@Success		200				{object}	LogsResponse
//	@Failure		400				{object}	ControlPlaneError
//	@Failure		401				{object}	ControlPlaneError
//	@Failure		403				{object}	ControlPlaneError
//	@Failure		404				{object}	ControlPlaneError
//	@Failure		429				{object}	ControlPlaneError
//	@Failure		500				{object}	ControlPlaneError
//	@Failure		503				{object}	ControlPlaneError
//	@Router			/v1/logs [get]
func (h *LogsHandler) GetLogs(c *gin.Context) {
	var params LogsRequest
	if err := c.ShouldBindQuery(&params); err != nil {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeLogsRetrievalFailed,
			Error:     "Invalid query parameters: " + err.Error(),
		})
		return
	}

	// Set default values if not provided
	if params.MaxResults == 0 {
		params.MaxResults = 100 // Default max results
	}

	// Read logs from file with pagination
	logs, nextToken, hasMore, err := h.readLogsFromFile(params.AppId, params.StartingToken, params.MaxResults)
	if err != nil {
		if err.Error() == "app not found" {
			c.PureJSON(http.StatusNotFound, &ControlPlaneError{
				ErrorCode: ErrCodeLogsRetrievalFailed,
				Error:     err.Error(),
			})
		} else {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeLogsRetrievalFailed,
				Error:     err.Error(),
			})
		}
		return
	}

	c.PureJSON(http.StatusOK, &LogsResponse{
		Logs:      logs,
		NextToken: nextToken,
		HasMore:   hasMore,
	})
}

// truncatingLineScanner wraps bufio.Reader to provide scanner-like interface with line truncation.
//
// Why: The default bufio.Scanner fails with a "token too long" error when encountering lines
// that exceed its buffer size (default 64KB). This is problematic for log files which may
// contain extremely long lines (e.g., large JSON payloads, stack traces, or malformed output).
// Rather than failing completely, we truncate long lines at 64KB and append "..." to indicate
// truncation, allowing log parsing to continue for subsequent lines.
type truncatingLineScanner struct {
	reader *bufio.Reader
	line   []byte
	err    error
}

// newTruncatingLineScanner creates a scanner that truncates long lines instead of failing
func newTruncatingLineScanner(r io.Reader) *truncatingLineScanner {
	size := 64 * 1024
	return &truncatingLineScanner{
		reader: bufio.NewReaderSize(r, size),
	}
}

// Scan reads the next line, truncating if necessary
func (s *truncatingLineScanner) Scan() bool {
	var fullLine []byte
	wasTruncated := false

	for {
		line, isPrefix, err := s.reader.ReadLine()
		if err != nil {
			s.err = err
			return false
		}

		// If this is the first chunk or we're still within limit, append it
		if len(fullLine) < s.reader.Size() {
			remaining := s.reader.Size() - len(fullLine)
			if len(line) > remaining {
				fullLine = append(fullLine, line[:remaining]...)
				wasTruncated = true
			} else {
				fullLine = append(fullLine, line...)
			}
		}

		// If line is complete (no prefix), we're done
		if !isPrefix {
			if wasTruncated {
				// Append ellipsis to indicate truncation
				fullLine = append(fullLine, []byte("...")...)
			}
			s.line = fullLine
			return true
		}

		// If we've already read maxLineLength, skip the rest
		if len(fullLine) >= s.reader.Size() {
			wasTruncated = true
			// Keep reading and discarding until we find the end of line
			for isPrefix {
				_, isPrefix, err = s.reader.ReadLine()
				if err != nil {
					s.err = err
					return false
				}
			}
			// Append ellipsis to indicate truncation
			fullLine = append(fullLine, []byte("...")...)
			s.line = fullLine
			return true
		}
	}
}

// Text returns the most recently read line
func (s *truncatingLineScanner) Text() string {
	return string(s.line)
}

// Err returns the first non-EOF error that occurred
func (s *truncatingLineScanner) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// findLinePosition performs binary search to find the approximate position of a target line number
func (h *LogsHandler) findLinePosition(file *os.File, targetLine int64) (int64, error) {
	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return 0, err
	}
	fileSize := fileInfo.Size()

	// Binary search bounds
	left := int64(0)
	right := fileSize
	var lastValidPos int64 = 0

	for left <= right {
		mid := (left + right) / 2

		// Seek to position
		if _, err := file.Seek(mid, 0); err != nil {
			return 0, fmt.Errorf("failed to seek to position %d: %w", mid, err)
		}

		// Read until we find a complete line
		scanner := newTruncatingLineScanner(file)
		if !scanner.Scan() {
			// If we can't read a line, move left
			right = mid - 1
			continue
		}

		// Find the line number
		lineMatch := h.lineRegex.FindStringSubmatch(scanner.Text())
		if len(lineMatch) < 2 {
			// If no line number found, move left
			right = mid - 1
			continue
		}

		currentLine, err := strconv.ParseInt(lineMatch[1], 10, 64)
		if err != nil {
			// If invalid line number, move left
			right = mid - 1
			continue
		}

		// Store the last valid position we found
		lastValidPos = mid

		if currentLine == targetLine {
			return mid, nil
		} else if currentLine < targetLine {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	// If we didn't find the exact line, return the last valid position
	return lastValidPos, nil
}

// readLogsFromFile reads logs from the log file with token-based pagination
func (h *LogsHandler) readLogsFromFile(appId string, startingToken int64, maxResults int) ([]LogEntry, int64, bool, error) {
	// Open log file
	logFilePath := h.getLogFilePath(appId)
	if logFilePath == "" {
		return []LogEntry{}, 1, false, fmt.Errorf("app not found")
	}
	file, err := os.Open(logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// If file doesn't exist, return empty logs
			return []LogEntry{}, 1, false, nil
		}
		return nil, 1, false, err
	}
	defer file.Close()

	// Find the approximate position of our target line to avoid reading the entire file
	startPos, err := h.findLinePosition(file, startingToken)
	if err != nil {
		return nil, 1, false, err
	}

	// Seek to the found position
	if _, err := file.Seek(startPos, 0); err != nil {
		return nil, 1, false, err
	}

	// Parse log lines
	var logs []LogEntry
	scanner := newTruncatingLineScanner(file)
	nextToken := int64(1)
	hasMore := false

	foundStart := false
	for scanner.Scan() {
		line := scanner.Text()
		lineMatch := h.lineRegex.FindStringSubmatch(line)
		if len(lineMatch) < 2 {
			continue
		}
		lineNum, err := strconv.ParseInt(lineMatch[1], 10, 64)
		if err != nil {
			continue
		}
		nextToken = lineNum + 1

		// If we haven't found our starting point yet, check if this is it
		if !foundStart && lineNum < startingToken {
			continue
		}

		foundStart = true

		// Extract timestamp
		timeMatch := h.timeRegex.FindStringSubmatch(line)
		var timestamp string
		if len(timeMatch) < 2 {
			timestamp = ""
		} else {
			timestamp = timeMatch[1]
		}

		// Extract message using regex
		messageMatch := h.messageRegex.FindStringSubmatch(line)
		var message string
		if len(messageMatch) < 2 {
			message = ""
		} else {
			message = strings.TrimSpace(messageMatch[1])
		}

		// Create log entry
		entry := LogEntry{
			LineNumber: lineNum,
			Timestamp:  timestamp,
			Message:    message,
		}
		logs = append(logs, entry)

		// If we've read maxResults + 1 lines, we know there are more
		if len(logs) > maxResults {
			hasMore = true
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, 1, false, err
	}

	if len(logs) == 0 {
		return []LogEntry{}, nextToken, false, nil
	}

	// Only return up to maxResults lines
	if len(logs) > maxResults {
		logs = logs[:maxResults]
		nextToken = logs[len(logs)-1].LineNumber + 1
	}

	return logs, nextToken, hasMore, nil
}

// DownloadLogs handles GET requests to download all logs as a zip file
//
//	@Summary		Download logs
//	@Description	Download all logs as a zip file
//	@Tags			Logs
//	@Produce		application/zip
//	@Success		200	{file}		file	"Zip file containing all logs"
//	@Failure		400	{object}	ControlPlaneError
//	@Failure		401	{object}	ControlPlaneError
//	@Failure		403	{object}	ControlPlaneError
//	@Failure		429	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Failure		503	{object}	ControlPlaneError
//	@Router			/v1/logs/download [get]
func (h *LogsHandler) DownloadLogs(c *gin.Context) {
	// Get the datasite
	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	// Get all app IDs
	appManager := ds.GetAppManager()
	apps, err := appManager.ListApps()
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeLogsRetrievalFailed,
			Error:     fmt.Sprintf("failed to list apps: %v", err),
		})
		return
	}

	// Create a temporary file for the zip
	tmpFile, err := os.CreateTemp("", "syftbox-logs-*.zip")
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeLogsRetrievalFailed,
			Error:     fmt.Sprintf("failed to create temporary file: %v", err),
		})
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Create a new zip writer
	zipWriter := zip.NewWriter(tmpFile)
	defer zipWriter.Close()

	// Add all logs from the system logs directory
	systemLogDir := filepath.Dir(config.DefaultLogFilePath)
	entries, err := os.ReadDir(systemLogDir)
	if err != nil {
		slog.Default().Warn("failed to read system logs directory", "error", err)
	} else {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
				logPath := filepath.Join(systemLogDir, entry.Name())
				if err := h.addFileToZip(zipWriter, logPath, entry.Name()); err != nil {
					slog.Default().Warn("failed to add log file to zip", "file", entry.Name(), "error", err)
					continue
				}
			}
		}
	}

	// Add app logs
	for _, app := range apps {
		appLogPath := filepath.Join(app.Path, "logs", "app.log")
		if err := h.addFileToZip(zipWriter, appLogPath, fmt.Sprintf("%s.log", app.ID)); err != nil {
			// Log the error but continue with other apps
			slog.Default().Warn("failed to add app logs to zip", "app", app.ID, "error", err)
			continue
		}
	}

	// Close the zip writer to ensure all data is written
	if err := zipWriter.Close(); err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeLogsRetrievalFailed,
			Error:     fmt.Sprintf("failed to finalize zip file: %v", err),
		})
		return
	}

	// Set response headers
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=syftbox-logs-%s.zip", time.Now().Format("2006-01-02-15-04-05")))
	c.Header("Content-Transfer-Encoding", "binary")

	// Send the file
	c.File(tmpFile.Name())
}

// addFileToZip adds a file to the zip archive if it exists
func (h *LogsHandler) addFileToZip(zipWriter *zip.Writer, filePath string, zipName string) error {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // Skip if file doesn't exist
	}

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	// Create a new file in the zip
	zipFile, err := zipWriter.Create(zipName)
	if err != nil {
		return fmt.Errorf("failed to create zip entry %s: %w", zipName, err)
	}

	// Copy the file contents to the zip
	if _, err := io.Copy(zipFile, file); err != nil {
		return fmt.Errorf("failed to copy file contents to zip: %w", err)
	}

	return nil
}
