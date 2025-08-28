# Access Control List (ACL) System

## Overview

The Access Control List (ACL) system in SyftBox provides fine-grained access control for file system operations. It allows users to define who can read, write, create, or administer files and directories through a hierarchical rule-based system.

### Key Components

- **ACLService**: Main service that manages and enforces access control rules
- **ACLTree**: Hierarchical tree structure for efficient rule lookups
- **ACLCache**: Performance optimization through caching of access decisions
- **ACLRule**: Individual access control rules with pattern matching
- **Matchers**: Pattern matching engines (exact, glob, template)

## How ACL Works

### 1. Hierarchical Rule Structure

The ACL system organizes rules in a tree structure where each node represents a directory path:

```
/
├── user1/
│   ├── public/          # Rules for user1/public/
│   └── private/         # Rules for user1/private/
└── user2/
    └── shared/          # Rules for user2/shared/
```

### 2. Rule Resolution Process

When a user requests access to a file, the system:

1. **Owner Check**: If the user owns the path, access is granted immediately
2. **Cache Lookup**: Check if the access decision is cached
3. **Tree Traversal**: Find the nearest node with applicable rules
4. **Pattern Matching**: Match the file path against rule patterns
5. **Permission Check**: Verify the user has the required access level
6. **Limit Validation**: Check file size and type restrictions
7. **Cache Storage**: Store the decision for future requests

### 3. Access Levels

The system supports four hierarchical access levels:

| Level | Value | Description |
|-------|-------|-------------|
| `AccessRead` | 1 | Read files and list directories |
| `AccessCreate` | 2 | Create new files and directories |
| `AccessWrite` | 4 | Modify existing files |
| `AccessAdmin` | 8 | Full administrative access |

Higher levels include lower level permissions (e.g., `AccessAdmin` includes all other permissions).

## Caching System

### Cache Architecture

The ACL system uses an LRU (Least Recently Used) cache with TTL (Time To Live) to optimize performance:

```go
const (
    aclCacheTTL        = time.Hour * 1
    aclAccessCacheSize = 100_000
)
```

### Cache Key Structure

Cache keys are generated using the pattern: `{path}:{user_id}:{access_level}`

This allows for:
- **User-specific caching**: Different users get different cache entries
- **Path-specific caching**: Each file path has its own cache entry
- **Level-specific caching**: Different access levels are cached separately

### Cache Operations

| Operation | Description | Use Case |
|-----------|-------------|----------|
| `Get()` | Retrieve cached access decision | Fast path for repeated requests |
| `Set()` | Store access decision | Cache successful/denied access |
| `Delete()` | Remove specific cache entry | File deletion cleanup |
| `DeletePrefix()` | Remove all entries under a path | ACL rule updates |

### Cache Invalidation Scenarios

1. **ACL File Updates**: When `syft.pub.yaml` files are modified
2. **File Deletion**: When files are deleted from the system
3. **TTL Expiration**: Automatic expiration after 1 hour
4. **LRU Eviction**: When cache reaches 100,000 entries

## ACL Rule Computation

### Rule Matching Process

1. **Path Normalization**: Convert path to standard format
2. **Tree Traversal**: Find the nearest node with rules
3. **Pattern Evaluation**: Match file path against rule patterns
4. **Template Resolution**: Resolve dynamic templates (if applicable)
5. **Permission Hierarchy**: Check admin → write → read permissions

### Example Rule Computation

```yaml
# syft.pub.yaml
rules:
  - pattern: "*.txt"
    access:
      admin: ["user1"]
      write: ["user2", "user3"]
      read: ["*"]
    limits:
      maxFileSize: 1048576  # 1MB
```

**Request**: `user2` wants to write to `/user1/documents/report.txt`

1. **Owner Check**: `user2` ≠ `user1` → continue
2. **Tree Lookup**: Find rules for `/user1/documents/`
3. **Pattern Match**: `report.txt` matches `*.txt` → rule applies
4. **Permission Check**: `user2` is in write list → access granted
5. **Limit Check**: File size within 1MB limit → proceed

### Template Resolution

The system supports dynamic templates using Go template syntax:

```yaml
rules:
  - pattern: "{{.UserHash}}/*"
    access:
      admin: ["USER"]
      read: ["*"]
```

**Example**: For user `alice@example.com`, this template resolves to:
- Pattern: `a1b2c3d4/*` (where `a1b2c3d4` is the SHA256 hash of "alice@example.com")
- Access: `admin: ["alice@example.com"]` (USER token replaced with actual user ID)
- Result: Alice gets full control over her directory, everyone can read

**Template Variables Available**:
- `{{.UserEmail}}`: User's email address
- `{{.UserHash}}`: SHA256 hash of user ID (first 8 bytes)
- `{{.Year}}`: Current year (YYYY format)
- `{{.Month}}`: Current month (MM format)
- `{{.Date}}`: Current day (DD format)

**Template Functions**:
- `{{sha2 "string"}}`: Generate SHA256 hash
- `{{sha2 "string" 16}}`: Generate truncated hash (16 chars)
- `{{upper "string"}}`: Convert to uppercase
- `{{lower "string"}}`: Convert to lowercase

## ACL Rule Templates and Examples

### 1. Public Read Access

```yaml
rules:
  - pattern: "**"
    access:
      admin: ["owner@example.com"]
      read: ["*"]
    limits:
      allowDirs: true
      allowSymlinks: false
```

**Use Case**: Public documentation or media files that anyone can read but only the owner can modify.

### 2. Private User Workspace

```yaml
rules:
  - pattern: "{{.UserHash}}/**"
    access:
      admin: ["USER"]
      read: ["USER"]
      write: ["USER"]
    limits:
      maxFileSize: 52428800  # 50MB
      allowDirs: true
```

**Use Case**: Personal workspace where each user gets their own isolated directory based on their user hash.

### 3. Shared Collaboration Space

```yaml
rules:
  - pattern: "shared/**"
    access:
      admin: ["admin@example.com"]
      write: ["user1@example.com", "user2@example.com", "user3@example.com"]
      read: ["*"]
    limits:
      maxFileSize: 104857600  # 100MB
      allowDirs: true
```

**Use Case**: Team collaboration where specific users can edit, everyone can read, and an admin manages the space.

### 4. File Type Restrictions

```yaml
rules:
  - pattern: "*.pdf"
    access:
      admin: ["owner@example.com"]
      read: ["*"]
    limits:
      maxFileSize: 10485760  # 10MB
      allowDirs: false
      allowSymlinks: false
```

**Use Case**: PDF documents that anyone can read but only the owner can manage, with size restrictions.

### 5. Time-Based Access

```yaml
rules:
  - pattern: "{{.Year}}/{{.Month}}/**"
    access:
      admin: ["USER"]
      write: ["USER"]
      read: ["*"]
    limits:
      allowDirs: true
```

**Use Case**: Time-organized content where users can manage their current month's content, but historical content is read-only for everyone.

### 6. Project-Based Access

```yaml
rules:
  - pattern: "projects/*/docs/**"
    access:
      admin: ["project-admin@example.com"]
      write: ["USER"]
      read: ["*"]
    limits:
      maxFileSize: 20971520  # 20MB
      allowDirs: true
```

**Use Case**: Project documentation where project members can edit docs, everyone can read, and a project admin has full control.

### 7. Secure File Storage

```yaml
rules:
  - pattern: "secure/**"
    access:
      admin: ["security-admin@example.com"]
      read: ["authorized-user1@example.com", "authorized-user2@example.com"]
    limits:
      maxFileSize: 5242880  # 5MB
      allowDirs: false
      allowSymlinks: false
```

**Use Case**: Sensitive files with strict access control and size limitations.

### 8. Public Upload Area

```yaml
rules:
  - pattern: "uploads/**"
    access:
      admin: ["admin@example.com"]
      write: ["*"]
      read: ["*"]
    limits:
      maxFileSize: 104857600  # 100MB
      allowDirs: true
```

**Use Case**: Public upload area where anyone can upload and download files, with an admin managing the space.

## Where ACL Rules Are Used

### 1. File Operations

- **Read Operations**: File downloads, directory listings
- **Write Operations**: File uploads, modifications
- **Create Operations**: New file/directory creation
- **Delete Operations**: File/directory removal

### 2. API Endpoints

The ACL system is integrated into various API endpoints:

- **Blob Handlers**: File upload/download operations
- **Datasite Handlers**: Workspace management
- **Explorer Handlers**: File browsing and navigation

### 3. Client-Side Enforcement

- **Sync Engine**: Ensures only authorized files are synced
- **File Watcher**: Monitors for changes and enforces permissions
- **App Manager**: Controls application access to files

### 4. Administrative Functions

- **ACL File Management**: Creation and modification of `syft.pub.yaml` files
- **User Management**: User access control and permissions
- **System Monitoring**: Access logging and audit trails

## Performance Considerations

### Optimization Strategies

1. **Caching**: LRU cache with TTL reduces rule computation overhead
2. **Tree Structure**: Hierarchical organization enables efficient lookups
3. **Pattern Matching**: Compiled patterns for fast matching
4. **Early Returns**: Owner checks bypass full rule evaluation

### Scalability Features

- **Concurrent Access**: Thread-safe cache and tree operations
- **Memory Management**: Configurable cache size and TTL
- **Efficient Storage**: Minimal memory footprint for rule storage
- **Batch Operations**: Support for bulk ACL updates

## Security Considerations

### Access Control Principles

1. **Principle of Least Privilege**: Users get minimum required access
2. **Defense in Depth**: Multiple layers of access control
3. **Audit Trail**: All access decisions are logged
4. **Secure Defaults**: Private access by default

### Security Features

- **Owner Override**: File owners always have full access
- **ACL File Protection**: Special handling for `syft.pub.yaml` files
- **Template Security**: Safe template execution with limited functions
- **Input Validation**: Comprehensive validation of ACL rules

## Troubleshooting

### Common Issues

1. **Access Denied**: Check user permissions and rule patterns
2. **Cache Issues**: Clear cache or wait for TTL expiration
3. **Template Errors**: Verify template syntax and variable usage
4. **Performance Problems**: Monitor cache hit rates and tree depth

### Debug Tools

- **Tree Visualization**: Use `ACLService.String()` for tree structure
- **Cache Statistics**: Monitor cache size and hit rates
- **Rule Matching**: Enable debug logging for rule evaluation
- **Template Resolution**: Test templates with sample data
