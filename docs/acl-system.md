# Access Control List (ACL) System

## Overview

The SyftBox ACL system provides fine-grained access control for files and directories within datasites. It uses a hierarchical rule-based system with YAML configuration files (`syft.pub.yaml`) to define permissions across read, write, and admin operations.

## Architecture

### Core Components

- **ACLService**: Main service managing ACL rules and access validation
- **ACLTree**: N-ary tree structure for efficient hierarchical ACL storage and lookup
- **ACLCache**: In-memory cache for rapid access control decisions
- **ACLSpec**: YAML-based ACL specification and parsing system
- **RuleSet**: Collection of rules applied to a specific path
- **Access**: Permission definitions for admin, read, and write operations

### System Design

The ACL system follows a hierarchical, path-based approach:

```
                    ACLService
                        │
        ┌───────────────┼───────────────┐
        │               │               │
    ACLTree         ACLCache      BlobService
        │                               │
    ┌───┴───┐                     ACL Files
    │ Nodes │                  (syft.pub.yaml)
    └───────┘
```

## ACL File Format

### File Location

ACL rules are defined in `syft.pub.yaml` files placed in directories throughout the datasite structure. Each file controls access to its directory and subdirectories.

### YAML Structure

```yaml
# Terminal flag - stops inheritance from parent directories
terminal: false

# Rules array - evaluated in order
rules:
  - pattern: "**/*.csv"     # Glob pattern for file matching
    access:
      admin: []             # Admin users (full control)
      write: ["user1"]      # Write permission users
      read: ["*"]           # Read permission users (* = everyone)
  
  - pattern: "private/**"
    access:
      admin: ["USER"]       # USER token = datasite owner
      write: []
      read: []
  
  - pattern: "**"           # Default rule (catch-all)
    access:
      admin: []
      write: []
      read: ["user2", "user3"]
```

### Special Tokens

- `USER`: Placeholder for the datasite owner
- `*`: Wildcard representing all users (public access)
- `**`: Glob pattern matching all files recursively

## Data Structures

### RuleSet

```go
type RuleSet struct {
    Rules    []*Rule  // Ordered list of access rules
    Terminal bool     // Stop traversing up the tree
    Path     string   // Directory path for this ruleset
}
```

### Rule

```go
type Rule struct {
    Pattern string   // Glob pattern for matching files
    Access  *Access  // Permission definitions
    Limits  *Limits  // Resource limits (future use)
}
```

### Access

```go
type Access struct {
    Admin mapset.Set[string]  // Users with admin permissions
    Read  mapset.Set[string]  // Users with read permissions
    Write mapset.Set[string]  // Users with write permissions
}
```

### ACLNode

```go
type ACLNode struct {
    path     string           // Full path of this node
    owner    string           // Owner of this path
    rules    []*ACLRule        // Compiled rules for this node
    terminal bool             // Terminal flag
    version  ACLVersion        // Version for cache invalidation
    depth    ACLDepth          // Tree depth (max 255)
    children map[string]*ACLNode // Child nodes
}
```

## Access Levels

The system defines four access levels with increasing privileges:

1. **AccessRead** (Level 1): Read file contents
2. **AccessCreate** (Level 2): Create new files
3. **AccessUpdate** (Level 3): Modify existing files
4. **AccessDelete** (Level 4): Remove files
5. **AccessAdmin** (Level 5): Full control including ACL modifications

### Permission Hierarchy

- **Read**: View file contents and metadata
- **Write**: Includes Create, Update, Delete operations
- **Admin**: Full control over files and ACL rules

## Tree-Based Lookup Algorithm

### Path Resolution

1. **Normalization**: Clean and normalize the requested path
2. **Tree Traversal**: Walk down the tree following path segments
3. **Terminal Nodes**: Stop at terminal nodes (no inheritance)
4. **Nearest Node**: Find the nearest node with defined rules
5. **Rule Matching**: Evaluate rules in order until a match is found

### Rule Evaluation Process

```
Request: /alice/projects/data.csv
User: bob
Level: AccessRead

1. Traverse: / → alice → projects
2. Find nearest node with rules: /alice/projects/
3. Load ruleset from syft.pub.yaml
4. Match patterns in order:
   - "**/*.csv" matches → check access
   - bob ∈ read users → ALLOW
```

### Inheritance and Terminal Nodes

- Rules inherit from parent directories by default
- Terminal nodes stop inheritance chain
- Most specific rule (deepest in tree) takes precedence

## Terminal Nodes: When and Why

### Understanding Terminal Nodes

Terminal nodes are a powerful feature that prevents the ACL system from looking further DOWN into subdirectories for ACL files. When a `syft.pub.yaml` file has `terminal: true`, it becomes the final authority for its directory and ALL subdirectories - no deeper ACL files will be evaluated.

### Benefits of Terminal Nodes

#### 1. **Simplified Permission Management**
- **Single Source of Truth**: One ACL file controls an entire directory tree
- **No Override Surprises**: Subdirectories cannot accidentally override parent rules
- **Easier Auditing**: All permissions defined in one place at the top level

#### 2. **Performance Optimization**
- **Faster Lookups**: Stops tree traversal early, no need to check deeper paths
- **Cache Efficiency**: Fewer ACL files to load and evaluate
- **Reduced Memory**: Only one ruleset needs to be compiled for entire subtree

#### 3. **Security Guarantees**
- **Enforced Boundaries**: Subdirectories cannot escalate privileges
- **Prevent Accidental Exposure**: No risk of a subdirectory ACL making data public
- **Centralized Control**: Owner maintains complete control from one location

### The Primary Use Case: Simplify Writeable Folders

The most common and recommended use is to simplify and secure a writeable or very private folder.

```yaml
# /alice/syft.pub.yaml
terminal: true  # This file controls EVERYTHING below
rules:
  - pattern: "public/**"
    access:
      read: ["*"]           # Public folder is globally readable
  
  - pattern: "shared/**"
    access:
      read: ["team-members"]
      write: ["trusted-collaborators"]

  - pattern: "**"           # Everything else is private
    access:
      read: []
      write: []
```

There is no need to think about what the rules are in these sub folders nor should it be possible for some strange exploit or program on your machine to accidentally write a `syft.pub.yaml` file to some sub folder and suddenly open up data or permissions it shouldn't.

## Caching Strategy

### Multi-Level Cache

1. **Access Cache**: Caches user+path+level → allow/deny decisions
2. **Rule Compilation Cache**: Caches compiled rules with resolved tokens
3. **Tree Node Cache**: In-memory tree structure for fast traversal

### Cache Invalidation

- Version-based invalidation on ruleset updates
- Prefix-based deletion for path changes
- Automatic cleanup on file deletions

## Security Considerations

### Owner Privileges

- Datasite owners have implicit full access to their files
- Owner check bypasses ACL evaluation for performance
- Owner determined from path prefix (first segment)

### ACL File Protection

- Modifying `syft.pub.yaml` requires admin permissions
- ACL file writes are automatically elevated to admin level
- Prevents privilege escalation through ACL manipulation

### File Limits and Extended Features

#### Limits Configuration

The ACL system supports resource limits to prevent abuse and manage storage efficiently:

```go
type Limits struct {
  MaxFileSize   int64  // Maximum file size in bytes, default: 0 no limit
  MaxFiles      uint32 // Maximum number of files, default: 0 no limit
  AllowDirs     bool   // Allow directory creation, default: true
  AllowSymlinks bool   // Allow symbolic links, default: false
}
```

#### Example: Storage Quotas

```yaml
# Limit uploads from external contributors
terminal: false
rules:
  - pattern: "contributions/**"
    access:
      write: ["*"]  # Anyone can contribute
      read: ["alice", "bob"]
    limits:
      maxFileSize: 10485760  # 10MB per file
      maxFiles: 100          # Max 100 files per user
      allowDirs: true
      allowSymlinks: false   # No symlinks for security
```

#### Example: Restricted Upload Area

```yaml
# Public upload area with strict limits
terminal: true
rules:
  - pattern: "uploads/temp/**"
    access:
      write: ["*"]
      read: ["admin"]
    limits:
      maxFileSize: 5242880  # 5MB max
      maxFiles: 10          # Only 10 files per user
      allowDirs: false      # No subdirectories
      allowSymlinks: false
```

#### Limits Enforcement

- Limits are checked during write operations (create/update)
- File size is validated before accepting uploads
- File count is tracked per user per directory
- Directory creation and symlinks can be controlled
- Default limits (0) mean no restriction

### Path Depth Limits

- Maximum tree depth: 255 levels
- Prevents deep nesting attacks
- Efficient u8 storage for depth values

## Performance Optimizations

### Parallel Processing

- Concurrent ACL file fetching (16 workers)
- Batch operations for bulk updates
- Non-blocking cache operations

### Memory Efficiency

- Lazy loading of ACL rules
- Prefix-based tree structure minimizes memory
- Efficient set operations for user lists

### Lookup Performance

- O(d) tree traversal where d = path depth
- O(1) cache hit for repeated access checks
- O(r) rule evaluation where r = rules per node

## Error Handling

### Common Errors

- `ErrNoRule`: No applicable rules found for path
- `ErrMaxDepthExceeded`: Path exceeds 255 levels
- `ErrInvalidRuleset`: Malformed YAML or missing required fields
- `ErrAccessDenied`: User lacks required permissions

### Graceful Degradation

- Missing ACL files default to private access
- Invalid rules are logged but don't crash the system
- Cache misses fall back to tree evaluation

## Default Datasite Conventions

### Initial Setup

When a new datasite is created, SyftBox automatically establishes a secure default structure with appropriate ACL configurations:

```
datasites/
└── alice/                      # User's datasite root
    ├── syft.pub.yaml          # Root ACL (private by default)
    └── public/                # Public folder
        └── syft.pub.yaml      # Public ACL (globally readable)
```

### Default Root ACL

The root datasite directory (`datasites/alice/`) receives a default ACL that makes everything private:

```yaml
# datasites/alice/syft.pub.yaml
terminal: false  # Non-terminal allows subdirectories to define their own ACLs
rules:
  - pattern: "**"
    access:
      admin: []    # Owner has implicit admin access
      write: []    # No one can write by default
      read: []     # No one can read by default
```

**Key Points:**
- Owner (alice) has implicit full access to all their files
- All content is private by default (principle of least privilege)
- Non-terminal by default to allow flexibility (users can add subdirectory ACLs)
- Users can change to `terminal: true` for simpler single-file management

### Default Public Folder

The `public/` folder is created with globally readable permissions:

```yaml
# datasites/alice/public/syft.pub.yaml
terminal: false  # Non-terminal allows further customization if needed
rules:
  - pattern: "**"
    access:
      admin: []    # Owner maintains admin control
      write: []    # No public writes (owner can still write)
      read: ["*"]  # Everyone can read
```

**Convention Benefits:**
- Clear separation between private and public data
- Familiar pattern (similar to ~/public_html in web servers)
- Safe default: must explicitly move data to public/
- Easy sharing: just place files in public/ folder

### Recommended: Single Terminal ACL

While the default setup uses non-terminal ACLs for flexibility, many users prefer a single terminal ACL at the root:

```yaml
# datasites/alice/syft.pub.yaml - Simplified approach
terminal: true  # One file controls everything
rules:
  - pattern: "public/**"
    access:
      read: ["*"]     # Public folder readable by all
  
  - pattern: "**"     # Everything else private
    access:
      read: []
      write: []
```

**Benefits of Terminal Root:**
- Simpler mental model
- Better performance
- No accidental permission leaks in subdirectories
- All permissions visible in one file

### Common Patterns

#### 1. **Shared Project Structure**
```
alice/
├── syft.pub.yaml              # Private root
├── public/                    # Public datasets
│   ├── syft.pub.yaml         # Read: everyone
│   └── datasets/
├── projects/                  # Collaborative projects
│   ├── research/
│   │   └── syft.pub.yaml     # Custom team permissions
│   └── development/
│       └── syft.pub.yaml     # Different team permissions
└── private/                   # Sensitive data
    └── syft.pub.yaml         # Terminal: true, strict access
```

### Security Recommendations

1. **Start Private**: Use default private root, explicitly grant access
2. **Use Public Folder**: Keep public data in designated `public/` directory
3. **Terminal for Sensitive Data**: Use terminal nodes for high-security zones
4. **Regular Audits**: Review ACL files periodically
5. **Test Permissions**: Verify access before sharing sensitive data

## Best Practices

### ACL File Placement

1. Place `syft.pub.yaml` at directory boundaries
2. Use terminal nodes to prevent inheritance
3. Keep rules simple and ordered by specificity

### Pattern Design

1. Most specific patterns first
2. Use `**` as catch-all default rule
3. Leverage glob patterns for file type control

### Security Guidelines

1. Principle of least privilege
2. Explicit deny over implicit allow
3. Regular audit of ACL configurations
4. Test access patterns before deployment

## Integration Points

### Blob Service

- ACL files stored as blobs
- Real-time updates via blob change events
- Efficient batch fetching on startup

### Sync Engine

- ACL validation before sync operations
- Permission checks for uploads/downloads
- Conflict resolution respects ACL rules

### WebSocket Events

- Real-time ACL update notifications
- Cache invalidation broadcasts
- Access denied event logging
