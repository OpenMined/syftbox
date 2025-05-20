package handlers

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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

func (h *LogsHandler) getLogFilePath(appName string) string {
	appName = strings.ToLower(appName)
	if appName == "" || appName == "system" {
		return config.DefaultLogFilePath
	}
	datasite, err := h.mgr.Get()
	if err != nil {
		return ""
	}
	appPath := filepath.Join(datasite.GetAppManager().AppsDir, appName)
	if !apps.IsValidApp(appPath) {
		return ""
	}
	return filepath.Join(appPath, "logs", "stdout.log")
}

// GetLogs handles GET requests to retrieve logs
//
//	@Summary		Get logs
//	@Description	Get system logs with pagination support
//	@Tags			Logs
//	@Produce		json
//	@Param			appName			query		string	false	"The name of the app to retrieve logs for"										default(system)
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
	logs, nextToken, hasMore, err := h.readLogsFromFile(params.AppName, params.StartingToken, params.MaxResults)
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
		scanner := bufio.NewScanner(file)
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
func (h *LogsHandler) readLogsFromFile(appName string, startingToken int64, maxResults int) ([]LogEntry, int64, bool, error) {
	// Open log file
	logFilePath := h.getLogFilePath(appName)
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
	scanner := bufio.NewScanner(file)
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
