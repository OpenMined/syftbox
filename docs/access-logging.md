# Access Logging

SyftBox Server provides comprehensive per-user access logging to track all file access attempts.

## Features

- **Per-User Logs**: Each user has their own log directory
- **Detailed Tracking**: Logs include timestamp, path, access type, IP, user-agent, and more
- **Automatic Rotation**: Logs rotate when they exceed 10MB, keeping max 5 files per user
- **JSON Format**: Structured logging in JSON for easy parsing and analysis
- **Server-side Only**: Logs are only accessible on the server filesystem for security

## Log Location

By default, logs are stored in `.logs/access/` in the current working directory (next to `.data`).

- **Default**: `.logs/access/` (same level as `.data` directory)
- **Custom**: Set via `--logDir` flag or `SYFTBOX_LOG_DIR` environment variable

Example directory structure with default setup:

```
~/
├── .data/                    # Database and state files
│   ├── state.db
│   ├── state.db-shm
│   └── state.db-wal
└── .logs/                    # Log files
    └── access/
        ├── user@example.com/
        │   ├── access_20241202.log          # Current day's log
        │   └── access_20241202_143022.log   # Rotated log
        └── another@user.org/
            └── access_20241202.log
```

## Configuration

### Command Line

```bash
# Use custom log directory
./syftbox_server --logDir /custom/path/to/logs
```

## Log Format

Each log entry is a JSON object with the following fields:

```json
{
	"timestamp": "2024-12-02 14:30:22.123 UTC",
	"path": "/user@example.com/private/file.txt",
	"access_type": "read",
	"user": "user@example.com",
	"ip": "192.168.1.100",
	"user_agent": "SyftBox/0.5.0-dev (HEAD; darwin/arm64; Go/go1.24.3; macOS/14.5.0)",
	"method": "GET",
	"status_code": 200,
	"allowed": true,
	"denied_reason": ""
}
```

### Access Types

- `read`: File read/download operation
- `write`: File write/upload operation
- `admin`: Administrative operation
- `deny`: Access was denied

## User-Agent Enhancement

The client now sends an enhanced User-Agent string with:

- SyftBox version and git revision
- Operating system and architecture
- Go runtime version
- OS-specific version details

Examples:

- macOS: `SyftBox/0.5.0-dev (HEAD; darwin/arm64; Go/go1.24.3; macOS/14.5.0)`
- Linux: `SyftBox/0.5.0-dev (abc123; linux/amd64; Go/go1.24.3; Ubuntu/22.04; kernel/5.15.0)`
- Windows: `SyftBox/0.5.0-dev (def456; windows/amd64; Go/go1.24.3; Windows/10.0.19044.2604)`

## Monitoring and Analysis

To grep logs for specific patterns:

```bash
# Find all denied access attempts for a user
grep '"allowed":false' .logs/access/user@example.com/*.log

# Find all write operations
grep '"access_type":"write"' .logs/access/user@example.com/*.log

# Parse with jq for structured queries
cat .logs/access/user@example.com/*.log | jq 'select(.allowed == false)'
```

## Security Considerations

- Log files have 0600 permissions (owner read/write only)
- Log directories have 0700 permissions (owner only)
- Usernames are sanitized for safe filesystem paths
- No sensitive data (passwords, tokens) is logged
- Logs are only accessible via server filesystem (no HTTP access)

## Log Rotation

- Logs rotate automatically when they exceed 10MB
- Maximum of 5 log files are kept per user
- Older logs are automatically deleted
- Daily log files are created with date-based naming
