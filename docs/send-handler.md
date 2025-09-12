# Send Handler and Service Documentation

## 1. Overview

### At a Glance
Updates:
- Default guest email is now `guest@syftbox.net` (legacy `guest@syft.org` still accepted).
- `.request` files include header `x-syft-url` with the original Syft URL.
- `Authorization` header is not forwarded to RPC endpoint (dropped from passthrough).
The Send Handler and Service components provide an HTTP-to-RPC bridge that enables asynchronous communication between clients and SyftBox applications. The system supports both online (WebSocket) and offline (polling) message delivery mechanisms.

### System Interaction Overview

```mermaid
sequenceDiagram
    participant Client as HTTP Client
    participant Handler as Send Handler
    participant Service as Send Service
    participant WS as WebSocket Hub
    participant Blob as Blob Storage
    participant App as SyftBox App
    participant Sync as Sync Engine

    Note over Client, Sync: Online Flow (Immediate Response)
    Client->>Handler: POST /api/v1/send/msg
    Handler->>Service: SendMessage()
    Service->>Service: Create RPC Message
    Service->>WS: Dispatch Message
    WS->>Sync: WebSocket Message
    Sync->>App: Write RPC File
    App->>App: Process Request
    App->>Sync: Create Response File
    Sync->>Blob: Upload Response
    Service->>Blob: Check Response
    Service->>Handler: Return Response
    Handler->>Client: Immediate Response

    Note over Client, Sync: Offline Flow (Polling)
    Client->>Handler: POST /api/v1/send/msg
    Handler->>Service: SendMessage()
    Service->>Service: Create RPC Message
    Service->>WS: Dispatch Message (Fails)
    Service->>Blob: Store Request
    Service->>Handler: Return Poll URL
    Handler->>Client: 202 Accepted + Poll URL
    
    Client->>Handler: GET /api/v1/send/poll
    Handler->>Service: PollForResponse()
    Service->>Blob: Check Response
    alt Response Available
        Service->>Handler: Return Response
        Handler->>Client: 200 OK + Response
    else No Response
        Service->>Handler: Return Timeout
        Handler->>Client: 202 Accepted + Retry
    end
```

### Purpose
- **HTTP-to-RPC Bridge**: Converts HTTP requests into RPC messages that can be delivered to SyftBox applications
- **Asynchronous Communication**: Supports both immediate responses (online) and delayed responses (offline polling)
- **Message Persistence**: Stores RPC messages in blob storage for reliable delivery
- **WebSocket Integration**: Provides real-time message delivery when clients are online

### Key Benefits
- **Offline Support**: Messages are stored and can be retrieved when clients come online
- **Reliable Delivery**: Uses blob storage for message persistence
- **Flexible Response Handling**: Supports both immediate and polling-based response retrieval
- **Cross-Platform**: Works with any HTTP client

### Scope
**Implemented Features:**
- HTTP request to RPC message conversion
- WebSocket message dispatch
- Blob storage for message persistence
- Polling-based response retrieval
- HTML and JSON response formats
- Request/response cleanup
- ACL permission checking for message sending and polling
- User-partitioned request/response storage with backward compatibility
- Sender suffix support for enhanced security

**Not Implemented:**
- TODO: Header filtering for security
- Large file uploads/downloads via blob APIs

## 2. Architecture

### System Design

```mermaid
graph TD
    A[HTTP Client] --> B[Send Handler]
    B --> C[Send Service]
    C --> D[Message Dispatcher]
    C --> E[Blob Storage]
    D --> F[WebSocket Hub]
    F --> G[Online Client]
    E --> H[Offline Storage]
    I[Polling Client] --> J[Poll Handler]
    J --> C
    C --> H
    H --> J
```

### Core Components

#### Send Handler (`send_handler.go`)
- **Purpose**: HTTP request handling and response formatting
- **Key Methods**:
  - `SendMsg()`: Processes incoming HTTP requests
  - `PollForResponse()`: Handles polling requests for responses
  - `readRequestBody()`: Validates and reads request bodies

#### Send Service (`send_service.go`)
- **Purpose**: Business logic for message processing and delivery
- **Key Methods**:
  - `SendMessage()`: Creates RPC messages and handles delivery
  - `PollForResponse()`: Retrieves stored responses
  - `checkForResponse()`: Handles online response processing
  - `pollForObject()`: Polls blob storage for objects

#### Message Store (`msg_store.go`)
- **Purpose**: Blob storage interface for RPC messages
- **Implementation**: `BlobMsgStore` using the blob service
- **Operations**: Store, retrieve, and delete messages

#### WebSocket Dispatcher (`ws_dispatch.go`)
- **Purpose**: Real-time message delivery via WebSocket
- **Implementation**: `WSMsgDispatcher` using the WebSocket hub

### Data Flow

#### Message Send Flow
1. **HTTP Request**: Client sends HTTP request to `/api/v1/send/msg`
2. **Request Processing**: Handler validates and extracts request data
3. **RPC Creation**: Service creates `SyftRPCMessage` from HTTP request
4. **Message Dispatch**: Attempts WebSocket delivery to online clients
5. **Storage**: Stores RPC message in blob storage
6. **Response**: Returns immediate response or poll URL

#### Response Retrieval Flow
1. **Poll Request**: Client polls `/api/v1/send/poll` for response
2. **Storage Check**: Service checks blob storage for response file
3. **Response Processing**: Retrieves and formats response data
4. **Cleanup**: Removes request/response files from storage

#### Client-Side Response Flow
1. **Application Processing**: Local application processes the RPC request and creates response file
2. **Sync Upload**: Syftbox Client uploads response to cache server using normal sync upload operations
3. **Blob Storage**: Response is stored in blob storage via the sync engine
4. **Polling**: Client can then poll for the response using the poll endpoint

## 3. Technical Deep Dive

### Data Structures

#### MessageRequest
```go
type MessageRequest struct {
    SyftURL      utils.SyftBoxURL `form:"x-syft-url" binding:"required"`
    From         string           `form:"x-syft-from" binding:"required"`
    Timeout      int              `form:"timeout" binding:"gte=0"`
    AsRaw        bool             `form:"x-syft-raw" default:"false"`
    Method       string           // Set from request method
    Headers      Headers          // Set from request headers
    SuffixSender bool             `form:"suffix-sender" default:"false"` // If true, adds sender to endpoint path
}
```

#### PollObjectRequest
```go
type PollObjectRequest struct {
    RequestID string           `form:"x-syft-request-id" binding:"required"`
    From      string           `form:"x-syft-from" binding:"required"`
    SyftURL   utils.SyftBoxURL `form:"x-syft-url" binding:"required"`
    Timeout   int              `form:"timeout,omitempty" binding:"gte=0"`
    UserAgent string           `form:"user-agent,omitempty"`
    AsRaw     bool             `form:"x-syft-raw" default:"false"`
}
```

#### HttpMsg
```go
type HttpMsg struct {
    From    string            `json:"from"`
    SyftURL utils.SyftBoxURL  `json:"syft_url"`
    Method  string            `json:"method"`
    Headers map[string]string `json:"headers,omitempty"`
    Body    []byte            `json:"body,omitempty"`
    Id      string            `json:"id"`
    Etag    string            `json:"etag,omitempty"`
}
```

### Configuration

#### Send Service Config
```go
type Config struct {
    DefaultTimeout      time.Duration // Default: 1 second
    MaxTimeout          time.Duration // Default: 10 seconds
    ObjectPollInterval  time.Duration // Default: 200ms
    RequestCheckTimeout time.Duration // Default: 200ms
    MaxBodySize         int64         // Default: 4MB
}
```

**Note**: The default 4MB limit is designed for control messages and small payloads.

### Response Processing

The `unmarshalResponse` function handles response processing based on the `x-syft-raw` flag:

```go
func unmarshalResponse(bodyBytes []byte, asRaw bool) (map[string]interface{}, error) {
    if asRaw {
        // For raw mode, treat the entire response as raw JSON (body remains base64-encoded)
        var bodyJson map[string]interface{}
        err := json.Unmarshal(bodyBytes, &bodyJson)
        if err != nil {
            return nil, fmt.Errorf("failed to unmarshal raw response: %w", err)
        }
        return map[string]interface{}{"message": bodyJson}, nil
    }

    // For standard mode, unmarshal as SyftRPCMessage (body gets decoded)
    var rpcMsg syftmsg.SyftRPCMessage
    err := json.Unmarshal(bodyBytes, &rpcMsg)
    if err != nil {
        return nil, fmt.Errorf("failed to unmarshal RPC response: %w", err)
    }

    // Return the RPC message with decoded body
    return map[string]interface{}{"message": rpcMsg.ToJsonMap()}, nil
}
```

**Processing Logic:**
- **`asRaw=true`**: Treats the entire response as raw JSON, unmarshals directly into a map (body remains base64-encoded)
- **`asRaw=false`**: Unmarshals response as a `SyftRPCMessage` struct, which calls `UnmarshalJSON()` to decode the base64 body, then calls `ToJsonMap()` to return the full RPC structure with decoded body content

**Data Flow:**
1. `bodyBytes` contains the serialized `SyftRPCMessage` as JSON bytes
2. **Raw mode**: `json.Unmarshal(bodyBytes, &bodyJson)` → Returns raw JSON (body still base64-encoded)
3. **Standard mode**: `json.Unmarshal(bodyBytes, &rpcMsg)` → `rpcMsg.UnmarshalJSON()` decodes body → `rpcMsg.ToJsonMap()` returns processed structure

### ACL Integration and User Partitioning

#### Permission Checking
The send service integrates with the ACL system to enforce access control:

```go
// Check if the user has permission to send message to this application
if err := s.checkPermission(requestRelPath, req.From, acl.AccessWrite); err != nil {
    return nil, ErrPermissionDenied
}

// Check if user has read access to the request
if err := s.checkPermission(requestRelPath, req.From, acl.AccessRead); err != nil {
    return nil, ErrPermissionDenied
}
```

#### User Partitioning
The system supports two storage formats for backward compatibility:

**New Format (User-Partitioned):**
```
app_data/myapp/rpc/endpoint/
├── alice@company.com/
│   ├── request-id-1.request
│   └── request-id-1.response
└── bob@company.com/
    ├── request-id-2.request
    └── request-id-2.response
```

**Legacy Format (Shared):**
```
app_data/myapp/rpc/endpoint/
├── request-id-1.request
├── request-id-1.response
├── request-id-2.request
└── request-id-2.response
```

#### Backward Compatibility
The polling mechanism automatically checks both formats:

```go
func (s *SendService) getCandidateRequestPaths(req *PollObjectRequest) []string {
    filename := fmt.Sprintf("%s.request", req.RequestID)
    basePath := req.SyftURL.ToLocalPath()

    requestPaths := []string{
        // Try sender suffix path first (new request path)
        path.Join(basePath, req.From, filename),
        // Fallback to legacy path (old request path)
        path.Join(basePath, filename),
    }

    return requestPaths
}
```

#### ACL Rules Support
The new user-partitioned format supports ACL rules like:
```yaml
rules:
- pattern: '**/{{.UserEmail}}/*.request'
  access:
    read: ['USER']
    write: ['USER']
- pattern: '**/{{.UserEmail}}/*.response'
  access:
    read: ['USER']
    write: ['USER']
```

### API Reference

#### Send Message Endpoint
- **URL**: `/api/v1/send/msg`
- **Method**: Any HTTP method
- **Authentication**: JWT required (guest access allowed)

**Query Parameters:**
- `x-syft-url` (required): SyftBox URL for the target application
- `x-syft-from` (required): Sender datasite (email address)
- `timeout` (optional): Request timeout in milliseconds
- `x-syft-raw` (optional): Response format flag (default: false)
- `suffix-sender` (optional): If true, adds sender email to endpoint path for user partitioning (default: false)

**Headers:** All request headers except `Authorization` are forwarded to the RPC message

**Request Body:** Any content (up to 4MB by default)

**Note:** For large file uploads/downloads, use the blob API directly instead of HTTP-over-RPC.

**Authentication:** 
- JWT Bearer token required for authenticated users
- Use `guest@syftbox.net` as `x-syft-from` for guest access (legacy `guest@syft.org` still accepted; no Bearer token needed)

**ACL Permissions:**
- Users must have write permission to the target endpoint to send messages
- Users must have read permission to the request file to poll for responses
- Datasite owners have automatic access to all endpoints within their datasite

**Response Format Behavior:**
- **`x-syft-raw=false` (default)**: Response is unmarshaled as a `SyftRPCMessage` struct, which decodes the base64 body and returns the full RPC structure with decoded body as JSON
- **`x-syft-raw=true`**: Response is treated as raw JSON and returned directly (body remains base64-encoded)

**User Partitioning and Backward Compatibility:**
- **New Format (with `suffix-sender=true`)**: Requests/responses stored as `{endpoint}/{user-email}/{request-id}.{request|response}`
- **Legacy Format (default)**: Requests/responses stored as `{endpoint}/{request-id}.{request|response}`
- **Polling**: Automatically checks both new and legacy paths for backward compatibility
- **ACL Support**: New format supports user-specific ACL rules for enhanced security

**Response:**
```json
{
    "request_id": "uuid-string",
    "data": {
        "poll_url": "/api/v1/send/poll?x-syft-request-id=..."
    },
    "message": "Request has been accepted. Please check back later."
}
```

#### Poll Response Endpoint
- **URL**: `/api/v1/send/poll`
- **Method**: GET
- **Authentication**: JWT required (guest access allowed)

**Query Parameters:**
- `x-syft-request-id` (required): Request ID from send operation
- `x-syft-from` (required): Original sender datasite (email address)
- `x-syft-url` (required): Original SyftBox URL
- `timeout` (optional): Poll timeout in milliseconds
- `x-syft-raw` (optional): Response format flag (default: false)

**Response (Success):**
```json
{
    "request_id": "uuid-string",
    "data": {
        "message": {
            "id": "uuid",
            "sender": "sender-id",
            "url": "syft://...",
            "method": "POST",
            "status_code": 200,
            "body": {
                "result": "success",
                "data": "response-content"
            },
            "headers": {},
            "created": "2024-01-01T00:00:00Z",
            "expires": "2024-01-02T00:00:00Z"
        }
    }
}
```

**Response (Timeout):**
```json
{
    "error": "timeout",
    "message": "Polling timeout reached. The request may still be processing.",
    "request_id": "uuid-string"
}
```

#### Response Format Examples

**Standard Response (`x-syft-raw=false`):**
```json
{
    "request_id": "uuid-string",
    "data": {
        "message": {
            "id": "uuid",
            "sender": "user@example.com",
            "url": "syft://user@example.com/app_data/myapp/rpc/endpoint",
            "method": "POST",
            "status_code": 200,
            "body": {
                "result": "success",
                "data": {
                    "key": "value",
                    "count": 42
                },
                "timestamp": "2024-01-01T00:00:00Z"
            },
            "headers": {
                "Content-Type": "application/json"
            },
            "created": "2024-01-01T00:00:00Z",
            "expires": "2024-01-02T00:00:00Z"
        }
    }
}
```

**Raw Response (`x-syft-raw=true`):**
```json
{
    "request_id": "uuid-string",
    "data": {
        "message": {
            "id": "uuid",
            "sender": "user@example.com",
            "url": "syft://user@example.com/app_data/myapp/rpc/endpoint",
            "method": "POST",
            "status_code": 200,
            "body": "eyJyZXN1bHQiOiJzdWNjZXNzIiwiZGF0YSI6eyJrZXkiOiJ2YWx1ZSIsImNvdW50Ijo0Mn0sInRpbWVzdGFtcCI6IjIwMjQtMDEtMDFUMDA6MDA6MDBaIn0=",
            "headers": {
                "Content-Type": "application/json"
            },
            "created": "2024-01-01T00:00:00Z",
            "expires": "2024-01-02T00:00:00Z"
        }
    }
}
```

## 4. Practical Examples

### Use Case Scenarios

#### Use Case 1: Database Query Service (Authenticated User)

**Scenario**: A client wants to query a database through a SyftBox application that processes SQL queries and returns results. Uses user partitioning for data security.

**Request:**
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://alice@company.com/app_data/db-service/rpc/query&x-syft-from=alice@company.com&suffix-sender=true&timeout=5000" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -H "X-Request-ID: req-12345" \
  -d '{
    "sql": "SELECT * FROM users WHERE status = ?",
    "params": ["active"],
    "limit": 100
  }'
```

**Response (Online - Immediate):**
```json
{
    "request_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "data": {
        "message": {
            "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
            "sender": "alice@company.com",
            "url": "syft://alice@company.com/app_data/db-service/rpc/query",
            "method": "POST",
            "status_code": 200,
            "body": {
                "results": [
                    {"id": 1, "name": "John Doe", "status": "active"},
                    {"id": 2, "name": "Jane Smith", "status": "active"}
                ],
                "total": 100
            },
            "headers": {
                "Content-Type": "application/json",
                "X-Request-ID": "req-12345"
            },
            "created": "2024-01-15T10:30:00Z",
            "expires": "2024-01-16T10:30:00Z"
        }
    }
}
```

**Storage Structure:**
```
app_data/db-service/rpc/query/
└── alice@company.com/
    ├── a1b2c3d4-e5f6-7890-abcd-ef1234567890.request
    └── a1b2c3d4-e5f6-7890-abcd-ef1234567890.response
```

**ACL Rules Applied:**
```yaml
rules:
- pattern: '**/alice@company.com/*.request'
  access:
    read: ['alice@company.com']
    write: ['alice@company.com']
- pattern: '**/alice@company.com/*.response'
  access:
    read: ['alice@company.com']
    write: ['alice@company.com']
```

#### Use Case 2: Machine Learning Model Inference (Authenticated User)

**Scenario**: A client sends data to a machine learning model for prediction. Uses user partitioning for data privacy and model access control.

**Request:**
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://ml-team@research.com/app_data/sentiment-analysis/rpc/predict&x-syft-from=ml-team@research.com&suffix-sender=true" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -d '{
    "text": "I love this product! It works perfectly.",
    "model_version": "v2.1",
    "confidence_threshold": 0.8
  }'
```

**Response (Offline - Poll Required):**
```json
{
    "request_id": "b2c3d4e5-f6g7-8901-bcde-f23456789012",
    "data": {
        "poll_url": "/api/v1/send/poll?x-syft-request-id=b2c3d4e5-f6g7-8901-bcde-f23456789012&x-syft-url=syft://ml-team@research.com/app_data/sentiment-analysis/rpc/predict&x-syft-from=ml-team@research.com&x-syft-raw=false"
    },
    "message": "Request has been accepted. Please check back later."
}
```

**Poll Request:**
```bash
curl "https://syftbox.net/api/v1/send/poll?x-syft-request-id=b2c3d4e5-f6g7-8901-bcde-f23456789012&x-syft-url=syft://ml-team@research.com/app_data/sentiment-analysis/rpc/predict&x-syft-from=ml-team@research.com&timeout=10000" \
  -H "Authorization: Bearer <jwt-token>"
```

**Poll Response:**
```json
{
    "request_id": "b2c3d4e5-f6g7-8901-bcde-f23456789012",
    "data": {
        "message": {
            "id": "b2c3d4e5-f6g7-8901-bcde-f23456789012",
            "sender": "ml-team@research.com",
            "url": "syft://ml-team@research.com/app_data/sentiment-analysis/rpc/predict",
            "method": "POST",
            "status_code": 200,
            "body": {
                "prediction": "positive",
                "confidence": 0.95,
                "model_version": "v2.1",
                "processing_time": 1.2
            },
            "headers": {
                "Content-Type": "application/json",
                "X-Model-Version": "v2.1"
            },
            "created": "2024-01-15T10:35:00Z",
            "expires": "2024-01-16T10:35:00Z"
        }
    }
}
```

**Storage Structure:**
```
app_data/sentiment-analysis/rpc/predict/
└── ml-team@research.com/
    ├── b2c3d4e5-f6g7-8901-bcde-f23456789012.request
    └── b2c3d4e5-f6g7-8901-bcde-f23456789012.response
```

**ACL Rules Applied:**
```yaml
rules:
- pattern: '**/ml-team@research.com/*.request'
  access:
    read: ['ml-team@research.com']
    write: ['ml-team@research.com']
- pattern: '**/ml-team@research.com/*.response'
  access:
    read: ['ml-team@research.com']
    write: ['ml-team@research.com']
```

#### Use Case 2b: Guest Access - Public API Demo

**Scenario**: A guest user wants to try a public sentiment analysis API without authentication.

**Request:**
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://demo@syftbox.net/app_data/public-sentiment/rpc/analyze&x-syft-from=guest@syft.org" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "This is amazing!",
    "language": "en"
  }'
```

**Response (Immediate):**
```json
{
    "request_id": "f6g7h8i9-j0k1-2345-fghi-678901234567",
    "data": {
        "message": {
            "id": "f6g7h8i9-j0k1-2345-fghi-678901234567",
            "sender": "guest@syftbox.net",
            "url": "syft://demo@syftbox.net/app_data/public-sentiment/rpc/analyze",
            "method": "POST",
            "status_code": 200,
            "body": {
                "sentiment": "positive",
                "score": 0.85,
                "language": "en",
                "timestamp": "2024-01-15T10:55:00Z"
            },
            "headers": {
                "Content-Type": "application/json"
            },
            "created": "2024-01-15T10:55:00Z",
            "expires": "2024-01-16T10:55:00Z"
        }
    }
}
```

**Poll Request (if needed):**
```bash
curl "https://syftbox.net/api/v1/send/poll?x-syft-request-id=f6g7h8i9-j0k1-2345-fghi-678901234567&x-syft-url=syft://demo@syftbox.net/app_data/public-sentiment/rpc/analyze&x-syft-from=guest@syftbox.net"
```

#### Use Case 3: File Processing Service (Authenticated User)

**Scenario**: A client sends a small configuration file for processing (note: large files should use blob API directly). Uses user partitioning for configuration data security.

**Request:**
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://data-team@company.com/app_data/file-processor/rpc/validate&x-syft-from=data-team@company.com&suffix-sender=true" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -H "X-File-Type: config" \
  -d '{
    "file_content": "base64-encoded-small-config",
    "file_name": "config.json",
    "file_size": 2048,
    "validation_rules": ["json_schema", "required_fields"]
  }'
```

**Response (Immediate):**
```json
{
    "request_id": "c3d4e5f6-g7h8-9012-cdef-345678901234",
    "data": {
        "message": {
            "id": "c3d4e5f6-g7h8-9012-cdef-345678901234",
            "sender": "data-team@company.com",
            "url": "syft://data-team@company.com/app_data/file-processor/rpc/validate",
            "method": "POST",
            "status_code": 200,
            "body": {
                "valid": true,
                "validation_results": [
                    {"rule": "json_schema", "status": "passed"},
                    {"rule": "required_fields", "status": "passed"}
                ],
                "errors": []
            },
            "headers": {
                "Content-Type": "application/json",
                "X-File-Type": "config"
            },
            "created": "2024-01-15T10:40:00Z",
            "expires": "2024-01-16T10:40:00Z"
        }
    }
}
```

**Storage Structure:**
```
app_data/file-processor/rpc/validate/
└── data-team@company.com/
    ├── c3d4e5f6-g7h8-9012-cdef-345678901234.request
    └── c3d4e5f6-g7h8-9012-cdef-345678901234.response
```

**ACL Rules Applied:**
```yaml
rules:
- pattern: '**/data-team@company.com/*.request'
  access:
    read: ['data-team@company.com']
    write: ['data-team@company.com']
- pattern: '**/data-team@company.com/*.response'
  access:
    read: ['data-team@company.com']
    write: ['data-team@company.com']
```

#### Use Case 4: Configuration Update (Authenticated User)

**Scenario**: A client updates application configuration settings. Uses user partitioning for admin-level configuration security.

**Request:**
```bash
curl -X PUT "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://admin@company.com/app_data/config-manager/rpc/update&x-syft-from=admin@company.com&suffix-sender=true" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -H "X-Environment: production" \
  -d '{
    "config_key": "database.connection_pool",
    "new_value": {
      "max_connections": 50,
      "min_connections": 5,
      "timeout": 30
    },
    "restart_required": false
  }'
```

**Response (Immediate):**
```json
{
    "request_id": "d4e5f6g7-h8i9-0123-defg-456789012345",
    "data": {
        "message": {
            "id": "d4e5f6g7-h8i9-0123-defg-456789012345",
            "sender": "admin@company.com",
            "url": "syft://admin@company.com/app_data/config-manager/rpc/update",
            "method": "PUT",
            "status_code": 200,
            "body": {
                "success": true,
                "config_updated": true,
                "message": "Configuration updated successfully",
                "timestamp": "2024-01-15T10:45:00Z"
            },
            "headers": {
                "Content-Type": "application/json",
                "X-Environment": "production"
            },
            "created": "2024-01-15T10:45:00Z",
            "expires": "2024-01-16T10:45:00Z"
        }
    }
}
```

**Storage Structure:**
```
app_data/config-manager/rpc/update/
└── admin@company.com/
    ├── d4e5f6g7-h8i9-0123-defg-456789012345.request
    └── d4e5f6g7-h8i9-0123-defg-456789012345.response
```

**ACL Rules Applied:**
```yaml
rules:
- pattern: '**/admin@company.com/*.request'
  access:
    read: ['admin@company.com']
    write: ['admin@company.com']
- pattern: '**/admin@company.com/*.response'
  access:
    read: ['admin@company.com']
    write: ['admin@company.com']
```

#### Use Case 5: Health Check - Raw vs Standard Response (Authenticated User)

**Scenario**: A client performs a health check and compares raw vs standard response formats. Uses user partitioning for monitoring data isolation.

**Request (Standard Response):**
```bash
curl -X GET "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://monitoring@company.com/app_data/health-check/rpc/status&x-syft-from=monitoring@company.com&suffix-sender=true&x-syft-raw=false" \
  -H "Authorization: Bearer <jwt-token>" \
  -H "X-Check-Type: full"
```

**Response (Standard Format - `x-syft-raw=false`):**
```json
{
    "request_id": "e5f6g7h8-i9j0-1234-efgh-567890123456",
    "data": {
        "message": {
            "id": "e5f6g7h8-i9j0-1234-efgh-567890123456",
            "sender": "monitoring@company.com",
            "url": "syft://monitoring@company.com/app_data/health-check/rpc/status",
            "method": "GET",
            "status_code": 200,
            "body": {
                "status": "healthy",
                "timestamp": "2024-01-15T10:50:00Z",
                "services": {
                    "database": "ok",
                    "cache": "ok",
                    "external_api": "ok"
                },
                "metrics": {
                    "cpu_usage": 45.2,
                    "memory_usage": 67.8,
                    "disk_usage": 23.1
                }
            },
            "headers": {
                "Content-Type": "application/json",
                "X-Check-Type": "full"
            },
            "created": "2024-01-15T10:50:00Z",
            "expires": "2024-01-16T10:50:00Z"
        }
    }
}
```

**Request (Raw Response):**
```bash
curl -X GET "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://monitoring@company.com/app_data/health-check/rpc/status&x-syft-from=monitoring@company.com&suffix-sender=true&x-syft-raw=true" \
  -H "Authorization: Bearer <jwt-token>" \
  -H "X-Check-Type: full"
```

**Response (Raw Format - `x-syft-raw=true`):**
```json
{
    "request_id": "e5f6g7h8-i9j0-1234-efgh-567890123456",
    "data": {
        "message": {
            "id": "e5f6g7h8-i9j0-1234-efgh-567890123456",
            "sender": "monitoring@company.com",
            "url": "syft://monitoring@company.com/app_data/health-check/rpc/status",
            "method": "GET",
            "status_code": 200,
            "body": "eyJzdGF0dXMiOiJoZWFsdGh5IiwidGltZXN0YW1wIjoiMjAyNC0wMS0xNVQxMDo1MDowMFoiLCJzZXJ2aWNlcyI6eyJkYXRhYmFzZSI6Im9rIiwiY2FjaGUiOiJvayIsImV4dGVybmFsX2FwaSI6Im9rIn0sIm1ldHJpY3MiOnsiY3B1X3VzYWdlIjo0NS4yLCJtZW1vcnlfdXNhZ2UiOjY3LjgsImRpc2tfdXNhZ2UiOjIzLjF9fQ==",
            "headers": {
                "Content-Type": "application/json",
                "X-Check-Type": "full"
            },
            "created": "2024-01-15T10:50:00Z",
            "expires": "2024-01-16T10:50:00Z"
        }
    }
}
```

**Key Differences:**
- **Standard Response**: Includes full RPC message metadata (id, sender, url, method, status_code, headers, created, expires) with the body decoded from base64 and returned as JSON
- **Raw Response**: Returns the raw JSON representation of the RPC message (body remains base64-encoded)

#### Use Case 7: Backward Compatibility - Legacy Application Support

**Scenario**: An existing application uses the legacy storage format without user partitioning. The system automatically handles backward compatibility.

**Request (Legacy Format - No User Partitioning):**
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://legacy@company.com/app_data/old-app/rpc/process&x-syft-from=legacy@company.com" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -d '{
    "action": "process_data",
    "data": "legacy-format-data"
  }'
```

**Response (Immediate):**
```json
{
    "request_id": "i9j0k1l2-m3n4-5678-ijkl-901234567890",
    "data": {
        "message": {
            "id": "i9j0k1l2-m3n4-5678-ijkl-901234567890",
            "sender": "legacy@company.com",
            "url": "syft://legacy@company.com/app_data/old-app/rpc/process",
            "method": "POST",
            "status_code": 200,
            "body": {
                "result": "processed",
                "data": "legacy-format-data",
                "timestamp": "2024-01-15T12:10:00Z"
            },
            "headers": {
                "Content-Type": "application/json"
            },
            "created": "2024-01-15T12:10:00Z",
            "expires": "2024-01-16T12:10:00Z"
        }
    }
}
```

**Storage Structure (Legacy Format):**
```
app_data/old-app/rpc/process/
├── i9j0k1l2-m3n4-5678-ijkl-901234567890.request
└── i9j0k1l2-m3n4-5678-ijkl-901234567890.response
```

**Poll Request (Backward Compatible):**
```bash
curl "https://syftbox.net/api/v1/send/poll?x-syft-request-id=i9j0k1l2-m3n4-5678-ijkl-901234567890&x-syft-url=syft://legacy@company.com/app_data/old-app/rpc/process&x-syft-from=legacy@company.com" \
  -H "Authorization: Bearer <jwt-token>"
```

**Backward Compatibility Notes:**
- **No `suffix-sender` parameter**: Uses legacy storage format
- **Automatic Path Resolution**: Polling checks both new and legacy paths
- **ACL Rules**: Uses legacy ACL patterns for shared access
- **Migration Path**: Can be gradually migrated to user partitioning by adding `suffix-sender=true`

**Legacy ACL Rules:**
```yaml
rules:
- pattern: '**/*.request'
  access:
    read: ['*']
    write: ['*']
- pattern: '**/*.response'
  access:
    read: ['*']
    write: ['*']
```

#### Use Case 5b: Guest Access - Public Calculator Service

**Scenario**: A guest user wants to use a public calculator service without authentication.

**Request:**
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://demo@syftbox.net/app_data/calculator/rpc/compute&x-syft-from=guest@syft.org" \
  -H "Content-Type: application/json" \
  -d '{
    "operation": "multiply",
    "operands": [15, 23]
  }'
```

#### Use Case 6: User-Partitioned Storage with ACL (Authenticated User)

**Scenario**: A client wants to use user-partitioned storage for enhanced security with ACL rules.

**Request (with User Partitioning):**
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://secure@company.com/app_data/private-api/rpc/process&x-syft-from=alice@company.com&suffix-sender=true" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -d '{
    "sensitive_data": "encrypted-content",
    "operation": "decrypt_and_process"
  }'
```

**Response (Immediate):**
```json
{
    "request_id": "h8i9j0k1-l2m3-4567-hijk-890123456789",
    "data": {
        "message": {
            "id": "h8i9j0k1-l2m3-4567-hijk-890123456789",
            "sender": "alice@company.com",
            "url": "syft://secure@company.com/app_data/private-api/rpc/process",
            "method": "POST",
            "status_code": 200,
            "body": {
                "result": "success",
                "processed_data": "decrypted-content",
                "timestamp": "2024-01-15T12:05:00Z"
            },
            "headers": {
                "Content-Type": "application/json"
            },
            "created": "2024-01-15T12:05:00Z",
            "expires": "2024-01-16T12:05:00Z"
        }
    }
}
```

**Storage Structure:**
```
app_data/private-api/rpc/process/
└── alice@company.com/
    ├── h8i9j0k1-l2m3-4567-hijk-890123456789.request
    └── h8i9j0k1-l2m3-4567-hijk-890123456789.response
```

**ACL Rules Applied:**
```yaml
rules:
- pattern: '**/alice@company.com/*.request'
  access:
    read: ['alice@company.com']
    write: ['alice@company.com']
- pattern: '**/alice@company.com/*.response'
  access:
    read: ['alice@company.com']
    write: ['alice@company.com']
```

**Response (Immediate):**
```json
{
    "request_id": "g7h8i9j0-k1l2-3456-ghij-789012345678",
    "data": {
        "message": {
            "id": "g7h8i9j0-k1l2-3456-ghij-789012345678",
            "sender": "guest@syftbox.net",
            "url": "syft://demo@syftbox.net/app_data/calculator/rpc/compute",
            "method": "POST",
            "status_code": 200,
            "body": {
                "result": 345,
                "operation": "multiply",
                "operands": [15, 23],
                "timestamp": "2024-01-15T11:55:00Z"
            },
            "headers": {
                "Content-Type": "application/json"
            },
            "created": "2024-01-15T11:55:00Z",
            "expires": "2024-01-16T11:55:00Z"
        }
    }
}
```

### Normal Flow Example

#### 1. Send Message (Online Client)
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://user@datasite.com/app_data/myapp/rpc/endpoint&x-syft-from=user@datasite.com&suffix-sender=true" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -d '{"key": "value"}'
```

**Response (Immediate):**
```json
{
    "request_id": "123e4567-e89b-12d3-a456-426614174000",
    "data": {
        "message": {
            "id": "123e4567-e89b-12d3-a456-426614174000",
            "sender": "user@datasite.com",
            "url": "syft://user@datasite.com/app_data/myapp/rpc/endpoint",
            "method": "POST",
            "status_code": 200,
            "body": {
                "response": "success"
            },
            "headers": {
                "Content-Type": "application/json"
            },
            "created": "2024-01-15T12:00:00Z",
            "expires": "2024-01-16T12:00:00Z"
        }
    }
}
```

#### 2. Send Message (Offline Client)
```bash
curl -X POST "https://syftbox.net/api/v1/send/msg?x-syft-url=syft://user@datasite.com/app_data/myapp/rpc/endpoint&x-syft-from=user@datasite.com&suffix-sender=true" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <jwt-token>" \
  -d '{"key": "value"}'
```

**Response (Poll Required):**
```json
{
    "request_id": "123e4567-e89b-12d3-a456-426614174000",
    "data": {
        "poll_url": "/api/v1/send/poll?x-syft-request-id=123e4567-e89b-12d3-a456-426614174000&x-syft-url=syft://user@datasite.com/app_data/myapp/rpc/endpoint&x-syft-from=user@datasite.com&x-syft-raw=false"
    },
    "message": "Request has been accepted. Please check back later."
}
```

#### 3. Poll for Response
```bash
curl "https://syftbox.net/api/v1/send/poll?x-syft-request-id=123e4567-e89b-12d3-a456-426614174000&x-syft-url=syft://user@datasite.com/app_data/myapp/rpc/endpoint&x-syft-from=user@datasite.com" \
  -H "Authorization: Bearer <jwt-token>"
```

### Error Scenarios

#### 1. Invalid Request
```json
{
    "error": "invalid_request",
    "message": "failed to bind query parameters: Key: 'MessageRequest.SyftURL' Error:Field validation for 'SyftURL' failed on the 'required' tag"
}
```

#### 2. Request Too Large
```json
{
    "error": "invalid_request",
    "message": "request body too large: 5242880 bytes (max: 4194304 bytes)"
}
```

#### 3. Request Not Found
```json
{
    "error": "not_found",
    "message": "No request found.",
    "request_id": "invalid-uuid"
}
```

### HTML Polling Interface

When polling with `Content-Type: text/html`, the system returns an auto-refreshing HTML page:

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta http-equiv="refresh" content="1; url=/api/v1/send/poll?x-syft-request-id=...">
    <title>Processing Request - SyftBox</title>
</head>
<body>
    <h1>Processing Your Request</h1>
    <div class="message">
        <p>Your request is being processed. This page will automatically refresh in 1 seconds.</p>
        <p>If you are not redirected, <a href="/api/v1/send/poll?...">click here</a> to check the status.</p>
    </div>
</body>
</html>
```

## 5. Implementation Guide

### Setup and Configuration

#### 1. Service Initialization
```go
// Create message dispatcher
dispatcher := send.NewWSMsgDispatcher(websocketHub)

// Create message store
store := send.NewBlobMsgStore(blobService)

// Create ACL service
aclService := acl.NewACLService(blobService)

// Create send service
service := send.NewSendService(dispatcher, store, aclService, &send.Config{
    DefaultTimeout:      1 * time.Second,
    MaxTimeout:          10 * time.Second,
    ObjectPollInterval:  200 * time.Millisecond,
    RequestCheckTimeout: 200 * time.Millisecond,
    MaxBodySize:         4 << 20, // 4MB
})

// Create handler
handler := send.New(dispatcher, store, aclService)
```

#### 2. Route Registration
```go
sendG := r.Group("/api/v1/send")
sendG.Use(middlewares.JWTAuth(authService, true)) // Allow guest access
{
    sendG.Any("/msg", handler.SendMsg)
    sendG.GET("/poll", handler.PollForResponse)
}
```

### Security Considerations

#### Implemented Security Features
- **JWT Authentication**: All endpoints require valid JWT tokens
- **Request Size Limits**: Configurable maximum body size (default: 4MB)
- **Input Validation**: Required field validation for all parameters
- **Timeout Limits**: Configurable timeouts prevent resource exhaustion

#### TODO: Security Enhancements
- **Header Filtering**: TODO: Filter out headers that are not allowed
- **ACL Permissions**: TODO: Check if user has permission to send message to this application

### Performance Guidelines

#### Implemented Optimizations
- **Blob Index Checking**: Checks blob index before attempting retrieval
- **Background Cleanup**: Request/response cleanup happens asynchronously
- **Configurable Polling**: Adjustable polling intervals for different use cases
- **Connection Reuse**: Uses existing blob service connections

#### Configuration Tuning
```go
config := &send.Config{
    DefaultTimeout:      2 * time.Second,    // Increase for slower networks
    ObjectPollInterval:  100 * time.Millisecond, // Decrease for faster response
    MaxBodySize:         10 << 20,           // Increase for larger payloads
}
```

## 6. Integration with Sync Engine

### HTTP Message Processing

The sync engine integrates with the send handler through the `processHttpMessage` function:

#### Message Flow
1. **Send Service**: Creates `HttpMsg` with RPC message body
2. **WebSocket Dispatch**: Sends message to online clients
3. **Sync Engine**: Receives message via WebSocket
4. **File Creation**: Writes RPC message to local file system
5. **Application Processing**: Local application processes the RPC request
6. **Response Creation**: Application creates response file
7. **Sync Upload**: Client uploads response to cache server using normal sync upload operations

#### File Structure

**Legacy Format (Default):**
```
datasite/
└── app_data/
    └── appname/
        └── rpc/
            └── endpoint/
                ├── {request-id}.request  # RPC request file
                └── {request-id}.response # RPC response file
```

**User-Partitioned Format (with `suffix-sender=true`):**
```
datasite/
└── app_data/
    └── appname/
        └── rpc/
            └── endpoint/
                ├── user1@example.com/
                │   ├── {request-id}.request  # User-specific request file
                │   └── {request-id}.response # User-specific response file
                └── user2@example.com/
                    ├── {request-id}.request  # User-specific request file
                    └── {request-id}.response # User-specific response file
```

#### Sync Engine Integration Code
```go
func (se *SyncEngine) processHttpMessage(msg *syftmsg.Message) {
    httpMsg, ok := msg.Data.(*syftmsg.HttpMsg)
    if !ok {
        return
    }

    // Create request file path
    fileName := httpMsg.Id + ".request"
    relPath := filepath.Join(httpMsg.SyftURL.ToLocalPath(), fileName)
    
    // Write RPC message to file
    rpcLocalAbsPath := se.workspace.DatasiteAbsPath(relPath)
    err := writeFileWithIntegrityCheck(rpcLocalAbsPath, httpMsg.Body, httpMsg.Etag)
    
    // Update sync journal
    se.journal.Set(&FileMetadata{
        Path:         SyncPath(relPath),
        ETag:         httpMsg.Etag,
        Size:         int64(len(httpMsg.Body)),
        LastModified: time.Now(),
    })
}
```

## 7. Reference Materials

### Error Codes

| Error Code | Description | HTTP Status |
|------------|-------------|-------------|
| `timeout` | Polling timeout reached | 202 Accepted |
| `invalid_request` | Request validation failed | 400 Bad Request |
| `internal_error` | Server internal error | 500 Internal Server Error |
| `not_found` | Request not found | 404 Not Found |
| `permission_denied` | User lacks required permissions | 403 Forbidden |

### Limitations

#### Current Limitations
- **No ACL Enforcement**: TODO: Permission checking not implemented
- **Header Filtering**: `Authorization` header is intentionally not forwarded; other headers are forwarded
- **Fixed File Structure**: RPC files follow specific naming convention
- **Single Response**: Only one response per request is supported
- **File Size Restrictions**: Maximum 4MB per request/response (use blob API for larger files)

#### Performance Limitations
- **Polling Overhead**: Offline clients must poll for responses
- **Storage Cleanup**: Request/response files are cleaned up asynchronously
- **Memory Usage**: Large request bodies are held in memory during processing
- **Not Suitable for Large Files**: Large file support is not implemented.

### Future Enhancements

#### TODO Items from Code
- **Header Security**: Add header filtering for security
- **Enhanced Error Handling**: More granular error responses
- **Batch Operations**: Support for multiple message operations
- **Large File Support**: Support for sending large files as part of the message

### Troubleshooting

#### Common Issues

1. **Poll Timeout**
   - **Cause**: Client is offline or application is slow to respond
   - **Solution**: Increase timeout or implement retry logic

2. **Request Not Found**
   - **Cause**: Request ID is invalid or request was cleaned up
   - **Solution**: Verify request ID and check cleanup timing

3. **Body Too Large**
   - **Cause**: Request exceeds configured size limit
   - **Solution**: Reduce payload size or increase `MaxBodySize`

4. **WebSocket Delivery Failure**
   - **Cause**: Client is offline
   - **Solution**: Use polling mechanism for offline clients

5. **Permission Denied**
   - **Cause**: User lacks required ACL permissions for the endpoint
   - **Solution**: Check ACL rules and ensure user has appropriate access rights
   - **For User-Partitioned Storage**: Ensure ACL rules are configured for the user-specific paths

#### Debug Information
- **Request ID**: All operations include request ID for tracing
- **ETags**: Used for integrity checking and deduplication
- **Timestamps**: Request creation and expiration times
- **Sync Status**: File sync status available in sync engine logs
