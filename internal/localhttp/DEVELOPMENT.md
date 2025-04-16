# Development Guide for UI Bridge Server

This document provides detailed information for developers working on the UI Bridge server.

## Setup

1. Install required dependencies:

   ```sh
   # Run from project root
   bash ./add_deps.sh
   ```

2. Generate Swagger documentation:

   ```sh
   # Install swag tool if not already installed
   go install github.com/swaggo/swag/cmd/swag@latest

   # Generate docs
   swag init -g server.go -d ./internal/uibridge -o ./internal/uibridge/docs
   ```

3. Set up hot reloading:

   ```sh
   # Install air if not already installed
   go install github.com/air-verse/air@latest

   # Run with hot reloading
   air
   ```

## Architecture Details

### Request Flow

The server follows this request flow:

1. Request enters through Gin router
2. Passes through middleware chain:
   - Error handler (captures errors)
   - Recovery (handles panics)
   - Request logger (logs requests)
   - CORS handler (sets CORS headers)
   - Compression (compresses response)
   - Rate limiter (limits requests)
   - Timeout (adds request timeout)
   - Authentication (for protected routes)
3. Reaches controller handler
4. Controller calls service layer
5. Service performs business logic
6. Response flows back through middleware

### Error Handling

The error handling system uses a structured approach:

1. Controllers should never return raw errors to the client
2. Use the `errors` package to create structured errors
3. Use `ctx.Error(appErr)` to pass errors to the error handler middleware
4. The error handler middleware handles formatting and logging

Example:

```go
if err != nil {
    appErr := errors.Internal("Failed to perform operation", err)
    ctx.Error(appErr)
    return
}
```

### Rate Limiting

Rate limiting is implemented using the token bucket algorithm:

- Each client IP gets its own rate limiter
- The rate limit is configurable via CLI flags
- Exceeding the rate limit returns a 429 Too Many Requests response

### Timeouts

Request timeouts prevent long-running requests from consuming resources:

- The timeout is configurable via CLI flags
- Health endpoints are excluded from timeouts
- Timed-out requests return a 504 Gateway Timeout response

## Testing

To effectively test the UI Bridge server:

1. Test basic connectivity:

   ```sh
   curl http://localhost:YOUR_PORT/health
   ```

2. Test authenticated endpoints:

   ```sh
   # Get the token from the server output
   curl http://localhost:YOUR_PORT/v1/status?token=YOUR_TOKEN
   ```

3. Test rate limiting:

   ```sh
   # Run multiple requests quickly to trigger rate limiting
   for i in {1..100}; do curl -s http://localhost:YOUR_PORT/v1/status?token=YOUR_TOKEN & done
   ```

4. View Swagger docs at:

   ```sh
   http://localhost:YOUR_PORT/swagger/index.html
   ```
