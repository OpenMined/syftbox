package handlers

import (
	"bufio"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/config"
)

// LogsHandler handles log-related requests
type LogsHandler struct {
	logFilePath string
}

// NewLogsHandler creates a new handler for logs
func NewLogsHandler() *LogsHandler {
	return &LogsHandler{
		logFilePath: config.DefaultLogFilePath,
	}
}

// GetLogs handles GET requests to retrieve logs
//
//	@Summary		Get logs
//	@Description	Get system logs with pagination support
//	@Tags			Logs
//	@Produce		json
//	@Param			startingToken	query		int	false	"Number of bytes to skip"			default(0)
//	@Param			maxResults		query		int	false	"Maximum number of lines to read"	default(100)
//	@Success		200				{object}	LogsResponse
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
	logs, bytesRead, err := h.readLogsFromFile(params.StartingToken, params.MaxResults)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeLogsRetrievalFailed,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusOK, &LogsResponse{
		Logs:      logs,
		NextToken: bytesRead,
	})
}

// readLogsFromFile reads logs from the log file with pagination support
func (h *LogsHandler) readLogsFromFile(startingToken int64, maxResults int) ([]LogEntry, int64, error) {
	// Open log file
	file, err := os.Open(h.logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// If file doesn't exist, return empty logs
			return []LogEntry{}, 0, nil
		}
		return nil, 0, err
	}
	defer file.Close()

	// Seek to the starting token position
	if startingToken > 0 {
		if _, err := file.Seek(startingToken, 0); err != nil {
			return nil, 0, err
		}
	}

	// Parse log lines
	var logs []LogEntry
	scanner := bufio.NewScanner(file)
	bytesRead := startingToken

	// Regular expressions to extract components
	timeRegex := regexp.MustCompile(`time=([^\s]+)`)
	levelRegex := regexp.MustCompile(`level=([^\s]+)`)
	msgRegex := regexp.MustCompile(`msg="([^"]+)"`)

	for scanner.Scan() {
		line := scanner.Text()
		bytesRead += int64(len(line) + 1) // +1 for newline

		// Extract timestamp
		timeMatch := timeRegex.FindStringSubmatch(line)
		if len(timeMatch) < 2 {
			continue // Skip line if timestamp not found
		}
		timestamp := timeMatch[1]

		// Extract level
		levelMatch := levelRegex.FindStringSubmatch(line)
		if len(levelMatch) < 2 {
			continue // Skip line if level not found
		}
		levelStr := strings.ToLower(levelMatch[1])

		// Map level string to LogLevel
		var level LogLevel
		switch levelStr {
		case "debug":
			level = LogLevelDebug
		case "info":
			level = LogLevelInfo
		case "warn", "warning":
			level = LogLevelWarn
		case "error":
			level = LogLevelError
		default:
			level = LogLevelInfo // Default to info for unknown levels
		}

		// Extract message and rest of the content
		msgMatch := msgRegex.FindStringSubmatch(line)
		if len(msgMatch) < 2 {
			continue // Skip line if message not found
		}
		message := msgMatch[1]

		// Get the remaining part of the line after the msg
		restIndex := strings.Index(line, msgMatch[0]) + len(msgMatch[0])
		if restIndex < len(line) {
			// Add the remaining content to the message
			rest := strings.TrimSpace(line[restIndex:])
			if rest != "" {
				message += " " + rest
			}
		}

		// Create log entry
		entry := LogEntry{
			Timestamp: timestamp,
			Level:     level,
			Message:   message,
		}

		logs = append(logs, entry)

		// Check if we've reached the max results
		if len(logs) >= maxResults {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	if len(logs) == 0 {
		return []LogEntry{}, bytesRead, nil
	}

	return logs, bytesRead, nil
}
