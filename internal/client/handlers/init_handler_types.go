package handlers

// GetTokenRequest represents the request to get a token from the syftbox server.
type GetTokenRequest struct {
	Email     string `form:"email" binding:"required"`      // email of the user
	ServerURL string `form:"server_url" binding:"required"` // syftbox server url from where the token is requested
}

// InitDatasiteRequest represents the request to initialize a datasite.
type InitDatasiteRequest struct {
	Email      string `json:"email" binding:"required"`     // email of the user
	DataDir    string `json:"dataDir" binding:"required"`   // datasite directory
	ServerURL  string `json:"serverUrl" binding:"required"` // syftbox server url
	EmailToken string `json:"token" binding:"required"`     // email token of the user
}
