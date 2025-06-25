# SyftBox Permissions System

## Overview

SyftBox implements a simple but powerful file-based Access Control List (ACL) system that mimics Unix permissions while providing more granular control over distributed file systems. The system is designed to secure data sharing across federated networks while maintaining ease of use and flexibility.

## SyftBox Permissions in 1 Minute ⚡

SyftBox uses **email-based permissions** to control who can access your files. Each user has a datasite with a default private root permission:

### Default Folder Structure
```
/datasites/
└── your@email.org/
    ├── syft.pub.yaml          # Root permissions (private by default)
    ├── public/
    │   └── syft.pub.yaml      # Public files
    └── shared/
        └── syft.pub.yaml      # Shared with specific collaborators
```

### Default Root Permissions
Your root `/datasites/your@email.org/syft.pub.yaml` starts private:
```yaml
rules:
  - pattern: "**"
    access:
      read: []    # No one can read
      write: []   # No one can write
      admin: []   # No one can modify permissions
```

### Example Public and Shared Folders
Create `/datasites/your@email.org/public/syft.pub.yaml`:
```yaml
terminal: true # optional `terminal: true`, child directories don't inherit parent permissions
rules:
  - pattern: "**"
    access:
      read: ["*"]  # Everyone can read
```

Create `/datasites/your@email.org/shared/syft.pub.yaml`:
```yaml
terminal: true # optional When `terminal: true`, child directories don't inherit parent permissions
rules:
  - pattern: "**"
    access:
      read: ["alice@university.edu", "bob@research.org"]
      write: ["alice@university.edu"]
```

**Key Points:**
- **Email addresses** identify users (`alice@university.edu`)
- **Patterns** match files (`*.txt`, `folder/*`, `**` for everything)
- **Permissions**: `read` (view), `write` (edit), `admin` (change permissions)
- **`"*"`** means public access
- **Root is private** by default - create subfolders for sharing
- **More specific rules** override general ones



## Design Philosophy: Unix-Inspired Permissions

The SyftBox permission system draws inspiration from Unix file permissions but extends beyond the traditional user/group/other model to support:

- **Distributed users**: Multiple users identified by email addresses across different nodes
- **Hierarchical rules**: Directory-based permission inheritance
- **Pattern matching**: Glob-based file pattern permissions
- **Granular access**: Separate read, write, and admin permissions
- **Terminal inheritance**: Controlled permission propagation

## User Identity

In SyftBox, users are identified by their **email addresses**. This federated approach allows researchers, organizations, and collaborators from different institutions to securely share data while maintaining clear identity management. Examples of user identifiers include:

- `*` - Special identifier meaning "everyone" (public access)
- `alice@research.org` - A researcher at a research institution
- `bob@university.edu` - A professor at a university
- `team@company.com` - A team or service account at a company
- `*@company.com` - Email's can use glob patterns to allow org level access

## Architecture Overview

The permission system consists of several key components working together:

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   ACL Service   │────│   Rule Cache     │────│  Tree Structure │
│                 │    │                  │    │                 │
│ - Rule lookup   │    │ - O(1) lookups   │    │ - N-ary tree    │
│ - Access check  │    │ - Cache hits     │    │ - Path traversal│
│ - File limits   │    │ - Invalidation   │    │ - Rule storage  │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                    ┌─────────────────────┐
                    │   syft.pub.yaml     │
                    │   Configuration     │
                    │                     │
                    │ - Rules patterns    │
                    │ - Access grants     │
                    │ - Terminal flags    │
                    └─────────────────────┘
```

## Core Components

### 1. ACL Service (`internal/server/acl/acl.go`)

The `AclService` is the main orchestrator that manages access control:

```go
type AclService struct {
    tree  *Tree      // Hierarchical rule storage
    cache *RuleCache // Performance optimization
}
```

**Key methods:**
- `CanAccess()` - Primary access control check
- `GetRule()` - Rule lookup with caching
- `AddRuleSet()` - Dynamic rule management

### 2. Tree Structure (`internal/server/acl/tree.go`)

The permission tree stores rules in an n-ary tree for efficient path-based lookups:

```go
type Tree struct {
    root *Node
}
```

**Features:**
- **O(depth)** rule lookups
- **Terminal nodes** to prevent inheritance
- **Dynamic rule addition/removal**
- **Path-based traversal**

### 3. Rule System (`internal/server/acl/rule.go`)

Rules define the actual access control logic:

```go
type Rule struct {
    fullPattern string        // Complete path + glob pattern
    rule        *aclspec.Rule // Access specifications
    node        *Node         // Associated tree node
}
```

### 4. Access Levels (`internal/server/acl/level.go`)

The system defines five distinct access levels using bit flags:

```go
type AccessLevel uint8

const (
    AccessRead     AccessLevel = 1 << iota // 0001
    AccessCreate                           // 0010
    AccessWrite                           // 0100
    AccessReadACL                         // 1000
    AccessWriteACL                        // 10000
)
```

## Permission Files: `syft.pub.yaml`

### File Format

Permission files follow the YAML format and are always named `syft.pub.yaml`:

```yaml
terminal: true
rules:
  - pattern: "*.md"
    access:
      read: ["*"]
  - pattern: "private/*.txt"
    access:
      read: ["alice@research.org", "bob@university.edu"]
      write: ["alice@research.org"]
  - pattern: "**"
    access: {}
```

### Configuration Elements

#### Terminal Flag
- **Purpose**: Controls permission inheritance
- **Values**: `true` (stop inheritance) or `false` (allow inheritance)
- **Impact**: When `terminal: true`, child directories don't inherit parent permissions

#### Rules Array
Each rule contains:
- **`pattern`**: Glob pattern matching files/directories
- **`access`**: Permission specifications

#### Access Specifications
- **`admin`**: Can modify ACL files (`syft.pub.yaml`) - specified by email addresses
- **`write`**: Can create, modify, and delete files - specified by email addresses
- **`read`**: Can view and download files - specified by email addresses
- **Special value `"*"`**: Grants access to everyone (public access)

### Example Configurations

#### Public Documentation with Private Admin
```yaml
terminal: true
rules:
  - pattern: "docs/*.md"
    access:
      read: ["*"]
      write: ["maintainer@company.com"]
  - pattern: "admin/*"
    access:
      admin: ["admin@company.com"]
  - pattern: "**"
    access:
      read: ["team@company.com"]
```

#### Collaborative Project Structure
```yaml
terminal: false
rules:
  - pattern: "src/**/*.go"
    access:
      read: ["*"]
      write: ["dev1@company.com", "dev2@university.edu"]
  - pattern: "tests/**"
    access:
      read: ["*"]
      write: ["dev1@company.com", "qa@company.com"]
  - pattern: "config/*.yaml"
    access:
      admin: ["devops@company.com"]
  - pattern: "**"
    access: {}
```

#### Research Data Sharing
```yaml
terminal: true
rules:
  - pattern: "public_datasets/*"
    access:
      read: ["*"]
  - pattern: "shared_analysis/*.ipynb"
    access:
      read: ["alice@research.org", "bob@university.edu", "charlie@institute.org"]
      write: ["alice@research.org"]
  - pattern: "raw_data/*"
    access:
      read: ["alice@research.org", "bob@university.edu"]
      admin: ["data-owner@research.org"]
```

## Access Control Flow

### Step-by-Step Permission Check

1. **Owner Check** (`internal/server/acl/acl.go`)
   ```go
   if user.IsOwner {
       return nil  // Owners bypass all restrictions
   }
   ```

2. **Rule Lookup** (`internal/server/acl/acl.go`)
   - Check cache for O(1) performance
   - Traverse tree structure for O(depth) lookup
   - Find most specific matching rule

3. **ACL File Elevation** (`internal/server/acl/acl.go`)
   ```go
   if isAcl && level == AccessWrite {
       level = AccessWriteACL  // Elevate to admin requirement
   }
   ```

4. **File Limits Check** (`internal/server/acl/rule.go`)
   - Validate file size limits
   - Check directory permissions
   - Verify symlink restrictions

5. **Access Verification** (`internal/server/acl/rule.go`)
   - Check user permissions against required level
   - Handle permission hierarchy (admin > write > read)

### Permission Hierarchy

The system enforces a clear permission hierarchy:

```
Admin (AccessWriteACL)
  ├─ Can modify syft.pub.yaml files
  ├─ Full write access
  └─ Full read access
     │
Write (AccessWrite)
  ├─ Can create/modify/delete files
  ├─ Subject to file limits
  └─ Full read access
     │
Read (AccessRead)
  └─ Can view and download files
```

## Tree Structure and Inheritance

### Hierarchical Rule Storage

The tree structure mirrors the file system hierarchy:

```
/
├─ users/
│  ├─ alice/
│  │  ├─ syft.pub.yaml (terminal: true)
│  │  └─ documents/
│  └─ bob/
└─ shared/
   ├─ syft.pub.yaml (terminal: false)
   └─ projects/
      └─ project1/
         └─ syft.pub.yaml (terminal: true)
```

### Rule Resolution (`internal/server/acl/tree.go`)

The `GetNearestNodeWithRules()` method traverses up the tree to find applicable rules:

```go
func (t *Tree) GetNearestNodeWithRules(path string) *Node {
    parts := pathParts(path)
    var candidate *Node
    current := t.root
    
    for _, part := range parts {
        if current.IsTerminal() {
            break  // Stop at terminal nodes
        }
        
        child, exists := current.GetChild(part)
        if !exists {
            break
        }
        
        current = child
        if child.Rules() != nil {
            candidate = current
        }
    }
    
    return candidate
}
```

## Caching System

### Performance Optimization

The rule cache provides O(1) lookups for frequently accessed paths:

- **Cache hits**: Return immediately without tree traversal
- **Cache misses**: Perform tree lookup and cache result
- **Cache invalidation**: Clear affected entries when rules change

### Cache Management (`internal/server/acl/acl.go`)

```go
func (s *AclService) RemoveRuleSet(path string) bool {
    s.cache.DeletePrefix(path)  // Invalidate cache entries
    return s.tree.RemoveRuleSet(path)
}
```

## File Limits and Restrictions

### Supported Limits (`internal/aclspec/limits.go`)

```go
type Limits struct {
    MaxFileSize   int64  `yaml:"maxFileSize,omitempty"`
    MaxFiles      uint32 `yaml:"maxFiles,omitempty"`
    AllowDirs     bool   `yaml:"allowDirs,omitempty"`
    AllowSymlinks bool   `yaml:"allowSymlinks,omitempty"`
}
```

### Enforcement Logic (`internal/server/acl/rule.go`)

```go
func (r *Rule) CheckLimits(info *File) error {
    limits := r.rule.Limits
    
    if limits.MaxFileSize > 0 && info.Size > limits.MaxFileSize {
        return ErrFileSizeExceeded
    }
    
    if !limits.AllowDirs && (info.IsDir || strings.Count(info.Path, pathSep) > 0) {
        return ErrDirsNotAllowed
    }
    
    if !limits.AllowSymlinks && info.IsSymlink {
        return ErrSymlinksNotAllowed
    }
    
    return nil
}
```

## Pattern Matching

### Glob Pattern Support

The system supports powerful glob patterns for flexible file matching:

- **`**`** - Matches all files and directories recursively
- **`*.ext`** - Matches all files with specific extension
- **`dir/*`** - Matches all direct children of directory
- **`dir/**`** - Matches all descendants of directory
- **`specific.txt`** - Matches exact filename

### Pattern Examples

```yaml
rules:
  - pattern: "**/*.py"          # All Python files
    access:
      read: ["dev1@company.com", "dev2@university.edu"]
      
  - pattern: "tests/**"         # Everything in tests directory
    access:
      write: ["qa@company.com"]
      
  - pattern: "config.yaml"      # Specific file
    access:
      admin: ["devops@company.com"]
      
  - pattern: "public/*"         # Direct children only
    access:
      read: ["*"]
```

## Implementation Details

### User Representation (`internal/server/acl/types.go`)

```go
type User struct {
    ID      string  // User identifier (email address)
    IsOwner bool    // Owner bypass flag
}
```

### File Information (`internal/server/acl/types.go`)

```go
type File struct {
    Path      string  // File system path
    IsDir     bool    // Directory flag
    IsSymlink bool    // Symbolic link flag
    Size      int64   // File size in bytes
}
```

### Rule Set Management (`internal/aclspec/ruleset.go`)

```go
type RuleSet struct {
    Rules    []*Rule `yaml:"rules,omitempty"`
    Terminal bool    `yaml:"terminal,omitempty"`
    Path     string  `yaml:"-"`  // Internal use only
}
```

## Error Handling

The system defines specific error types for different access violations:

```go
var (
    ErrAdminRequired      = errors.New("admin access required")
    ErrWriteRequired      = errors.New("write access required")
    ErrReadRequired       = errors.New("read access required")
    ErrDirsNotAllowed     = errors.New("directories not allowed")
    ErrSymlinksNotAllowed = errors.New("symlinks not allowed")
    ErrFileSizeExceeded   = errors.New("file size exceeds limits")
)
```

## Testing and Validation

### Test Coverage

The system includes comprehensive tests covering:

- **Rule resolution** (`internal/server/acl/acl_test.go`)
- **Access control enforcement** (`internal/server/acl/acl_test.go`)
- **File limit validation** (`internal/server/acl/acl_test.go`)
- **Cache behavior** (`internal/server/acl/acl_test.go`)
- **Tree operations** (`internal/server/acl/tree_test.go`)

### Example Test Case

```go
func TestAclServiceCanAccess(t *testing.T) {
    service := NewAclService()
    
    ruleset := aclspec.NewRuleSet(
        "user",
        aclspec.SetTerminal,
        aclspec.NewRule("public/*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
        aclspec.NewRule("private/*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
    )
    
    err := service.AddRuleSet(ruleset)
    assert.NoError(t, err)
    
    // Test access control
    regularUser := &User{ID: aclspec.Everyone, IsOwner: false}
    publicFile := &File{Path: "user/public/doc.txt", Size: 100}
    
    err = service.CanAccess(regularUser, publicFile, AccessRead)
    assert.NoError(t, err)  // Should succeed
}
```

## Best Practices

### 1. Permission Design
- Start with restrictive permissions and grant access as needed
- Use terminal nodes to prevent unintended inheritance
- Group related files using consistent patterns

### 2. Rule Organization
- Place more specific rules before general ones
- Always include a default `**` catch-all rule
- Use descriptive user group names

### 3. Security Considerations
- Monitor for overly permissive `*` grants
- Implement file size limits for public areas

### 4. Performance Optimization
- Keep rule sets small and focused
- Use terminal nodes to limit tree traversal
- Monitor cache hit rates

## Integration Points

### Server Integration (`internal/server/server.go`)

The ACL service integrates with the SyftBox server to protect all file operations:

- **File uploads**: Check write permissions
- **File downloads**: Verify read access
- **Directory listings**: Filter based on permissions
- **ACL modifications**: Require admin access

### Client Synchronization (`internal/client/sync/`)

The sync engine respects ACL permissions during:

- **Upload operations**: Validate write access before sync
- **Download filtering**: Only sync permitted files
- **Conflict resolution**: Consider permission changes

## Migration and Compatibility

The system includes migration support for updating ACL formats:

- **Backward compatibility**: Support older permission formats
- **Automatic migration**: Upgrade rules during system updates
- **Validation**: Ensure rule consistency after migration

## Conclusion

The SyftBox permission system provides a robust, Unix-inspired access control mechanism tailored for distributed file systems. By combining hierarchical rules, pattern matching, and efficient caching, it offers both security and performance for federated data sharing scenarios.

The system's design emphasizes:
- **Flexibility**: Glob patterns and granular permissions
- **Performance**: O(1) cached lookups and O(depth) tree traversal
- **Security**: Default-deny policies and owner protections
- **Usability**: YAML configuration and inheritance patterns

This architecture enables secure, scalable data sharing across distributed networks while maintaining the intuitive permission model familiar to Unix users.