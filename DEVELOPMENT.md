# Development

Follow these steps to get your development environment set up.

### Install Toolchain

1.  **Install Go and tools:**
    ```sh
    # On MacOS with Homebrew
    brew install go
    brew install mkcert
    brew install FiloSottile/musl-cross/musl-cross
    go install github.com/air-verse/air@latest
    go install github.com/swaggo/swag/v2/cmd/swag@latest
    ```
    *(For other operating systems, please refer to the official Go documentation.)*

2.  **Install Just:**
    `just` is a command runner used in this project.
    ```sh
    # On MacOS with Homebrew
    brew install just

    # With Cargo (Rust's package manager)
    cargo install just

    # Or download from releases: https://github.com/casey/just/releases
    ```

### IDE Setup (VSCode/Cursor Recommended)

Install the official Go extension. Then, add the following to your user `settings.json`:
```json
{
    "go.toolsManagement.autoUpdate": true,
    "gopls": {
        "ui.semanticTokens": true
    }
}
```

### Common Development Commands

List all available commands:
```sh
just
```

Run the client:
```sh
just run-client -e <email> -d <datadir> -s <server> -c <configpath>
# Or directly:
go run ./cmd/client -e <email> -d <datadir> -s <server> -c <configpath>
```

Run the server (using `./config/server.dev.yaml`):
```sh
just run-server -f ./config/server.dev.yaml
# Or directly:
go run ./cmd/server -f ./config/server.dev.yaml
```

Run the server with TLS (generates certs if needed):
```sh
just run-server-tls
# Or directly (ensure certs exist, e.g., via `just gen-certs`):
just gen-certs
go run ./cmd/server --cert certs/cert.pem --key certs/cert.key
```

Run a local server with two separate clients
```sh
go run ./cmd/server
go run ./cmd/client -e user1@email.com -s http://localhost:8080 -d ~/SyftBox.user1 -c ~/.syftbox/config.user1.json
go run ./cmd/client -e user2@email.com -s http://localhost:8080 -d ~/SyftBox.user2 -c ~/.syftbox/config.user2.json
```

Run tests:
```sh
just test
# (The `just test` command also generates an HTML coverage report)

# Or directly (basic tests):
go test ./...
```

### Working with MinIO (Local S3 Storage)

Start MinIO container:
```sh
just run-minio
```

Stop and delete MinIO container and data:
```sh
just destroy-minio # Use with caution!
```

SSH into the running MinIO container:
```sh
just ssh-minio
```

### Building Binaries

Build client binary for your current OS/Arch:
```sh
# Example: Build for Linux ARM64 with specific compiler
# CC="aarch64-linux-musl-gcc" just build-client-target goos=linux goarch=arm64

just build-client-target
# (The `just` command includes specific build tags and version flags)

# Or directly (basic build for current platform):
go build -o .out/syftbox_client ./cmd/client
```

Build client binaries using GoReleaser (for configured targets):
```sh
just build-client
```

Build server binary using GoReleaser:
```sh
just build-server
```

Build all binaries using GoReleaser:
```sh
just build-all
```
