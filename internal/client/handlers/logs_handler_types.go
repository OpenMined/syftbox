package handlers

// LogEntry represents a single log message
type LogEntry struct {
	LineNumber int64  `json:"lineNumber"`
	Timestamp  string `json:"timestamp"`
	Message    string `json:"message"`
}

// LogsRequest represents the request parameters for retrieving logs
type LogsRequest struct {
	// The name of the app to retrieve logs for.
	AppName string `form:"appName" binding:"required" default:"system"`
	// Specify the pagination token from a previous request to retrieve the next page of results.
	StartingToken int64 `form:"startingToken" binding:"min=1" default:"1"`
	// The maximum number of logs to return in one page of results.
	MaxResults int `form:"maxResults" binding:"gte=1,lte=1000" default:"100"`
}

// LogsResponse represents the response for retrieving logs
type LogsResponse struct {
	// A list of log items.
	Logs []LogEntry `json:"logs"`
	// A pagination token to retrieve the next page of logs.
	NextToken int64 `json:"nextToken"`
	// Whether there are more logs to retrieve.
	HasMore bool `json:"hasMore"`
}

// ErrCodeLogsRetrievalFailed is the error code for when logs cannot be retrieved
const ErrCodeLogsRetrievalFailed = "ERR_LOGS_RETRIEVAL_FAILED"
