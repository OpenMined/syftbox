package auth

// OTPRequest is the request for an OTP code to be sent to the user's email.
type OTPRequest struct {
	Email string `json:"email" binding:"required"`
}

// OTPRequestResponse is the response for an OTP code to be sent to the user's email.
type OTPRequestResponse struct {
	Email string `json:"email"`
}

// OTPVerifyRequest is the request for a verified OTP code.
type OTPVerifyRequest struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// OTPVerifyResponse is the response for a verified OTP code.
type OTPVerifyResponse RefreshResponse

// RefreshRequest is the request for a new access token.
type RefreshRequest struct {
	OldRefreshToken string `json:"refreshToken" binding:"required"`
}

// RefreshResponse is the response for a new access token.
type RefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}
