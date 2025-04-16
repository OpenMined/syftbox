# SyftBox UI Bridge

The UI Bridge provides a RESTful API interface for SyftUI, and allows web applications to communicate securely with the SyftBox client.

## Architecture

The UI Bridge server follows a clean, modular architecture with these main components:

- **Controllers**: Handle HTTP requests and responses
- **Services**: Contain business logic
- **Models**: Define data structures
- **Middleware**: Provide cross-cutting concerns like authentication, logging and rate limiting
- **Errors**: Structured error handling

## Features

### Security

- Token-based authentication
- Rate limiting protection against abuse
- Request timeouts to prevent resource exhaustion

### Performance

- Response compression with Gzip
- Optimized middleware chain

### Developer Experience

- Structured error handling
- OpenAPI/Swagger documentation
- Hot reloading support

## Authentication

All API endpoints (except health check) require authentication using a token. The token can be found in the CLI's output, and can be provided in:

1. Query parameter: `?token=YOUR_TOKEN`
2. Authorization header: `Authorization: Bearer YOUR_TOKEN`

## Configuration

The UI Bridge can be configured with the following CLI flags:

```sh
--ui                  # Enable the UI bridge server (default: true)
--ui-host string      # Host to bind the UI bridge server (default: "localhost")
--ui-port int         # Port for the UI bridge server (default: 0, for allocating a randomly available port)
--ui-token string     # Access token (default: randomly generated)
--ui-swagger          # Enable Swagger documentation (default: false)
--ui-timeout          # Request timeout (default: 30s)
--ui-rate-limit       # Rate limit in requests per second (default: 10)
--ui-rate-burst       # Maximum burst size for rate limiting (default: 20)
```

## Swagger Documentation

When the `--ui-swagger` flag is enabled, Swagger documentation is automatically generated and available at `/swagger/index.html`. This provides:

- Interactive API documentation
- Request/response schemas
- Try-it-out capability to test endpoints directly

### How to Use Swagger UI

1. Start the client with Swagger enabled:

   ```sh
   air -- --email user@example.com --ui-port 8000 --ui-swagger
   ```

2. Open the provided URL in your browser.

3. Navigate to `http://localhost:8000/swagger/index.html` to see the API documentation.

4. For authenticated endpoints, click **Authorize** and enter your token from the CLI output.

## Development

### Prerequisites

- Go 1.23 or higher

### Running with Hot Reload

For development, you can use Air for hot reloading:

```sh
go install github.com/air-verse/air@latest
air
```

### Generating Swagger Docs Manually

To manually generate the Swagger documentation:

```sh
go run scripts/generate_swagger.go
```

Or using the swag CLI directly:

```sh
go install github.com/swaggo/swag/cmd/swag@latest
swag init -g server.go -d ./internal/uibridge -o ./internal/uibridge/docs
```

## CLI Usage

Start the UI Bridge server with the syftgo client at a random port:

```sh
air -- --email user@example.com
```

Start the UI Bridge server with custom host and port:

```sh
air -- --email user@example.com --ui-host 0.0.0.0 --ui-port 5001
```

Enable Swagger documentation:

```sh
air -- --email user@example.com --ui-swagger
```

Configure security settings:

```sh
air -- --email user@example.com --ui-rate-limit 5 --ui-timeout 60s
```
