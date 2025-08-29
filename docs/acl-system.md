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
      write: ["user1@example.com"]      # Write permission users
      read: ["*"]           # Read permission users (* = everyone)
  
  - pattern: "private/**"
    access:
      admin: []             # Only datasite owner can access it.
      write: []             # No read access
      read: []              # No write access 
  
  - pattern: "**"           # Default rule (catch-all)
    access:
      admin: []
      write: []
      read: ["user2@example.com", "user3@example.com"]
```

### Special Tokens

- `*`: Wildcard representing all users (public access)
- `**`: Glob pattern matching all files recursively

## Data Structures

### Core Service Structure

#### ACLService
The main service orchestrating all ACL operations:

```go
type ACLService struct {
    blob  BlobService      // Interface to fetch ACL files
    tree  *ACLTree         // N-ary tree storing all rules
    cache *ACLCache        // High-performance lookup cache
}
```

### Tree Components

#### ACLTree
N-ary tree structure for hierarchical rule storage:

```go
type ACLTree struct {
    root *ACLNode          // Root node of the tree
}
```

#### ACLNode
Each node represents a path segment in the tree:

```go
type ACLNode struct {
    sync.RWMutex                    // Thread-safe operations
    path     string                 // Full path (e.g., "alice/projects")
    owner    string                 // Extracted from first segment
    rules    []*ACLRule              // Sorted by specificity (most specific first)
    terminal bool                   // Stops traversal if true
    version  ACLVersion              // Incremented on changes for cache invalidation
    depth    ACLDepth                // Tree depth (0-255)
    children map[string]*ACLNode     // Child nodes by path segment
}
```

Example node structure in memory:
```
root (path: "/", owner: "")
├── alice (path: "alice", owner: "alice")
│   ├── rules: [
│   │     ACLRule{fullPattern: "alice/**/*.csv", rule: {pattern: "**/*.csv", access: {read: ["*"]}}},
│   │     ACLRule{fullPattern: "alice/**", rule: {pattern: "**", access: {read: []}}}
│   │   ]
│   ├── public (path: "alice/public", owner: "alice")
│   │   └── rules: [ACLRule{fullPattern: "alice/public/**", rule: {pattern: "**", access: {read: ["*"]}}}]
│   └── private (path: "alice/private", owner: "alice", terminal: true)
│       └── rules: [ACLRule{fullPattern: "alice/private/**", rule: {pattern: "**", access: {read: [], write: []}}}]```

#### ACLRule
Compiled rule with resolved patterns:

```go
type ACLRule struct {
    fullPattern string    // Complete pattern (e.g., "alice/public/**/*.csv")
    rule        *Rule     // Original rule from YAML
    node        *ACLNode  // Parent node reference
}
```

### Rule Specification Structures

#### RuleSet
Represents a complete ACL file:

```go
type RuleSet struct {
    Rules    []*Rule  // Ordered list of access rules
    Terminal bool     // Stop traversing down the tree
    Path     string   // Directory path for this ruleset
}
```

#### Rule
Individual access rule from YAML:

```go
type Rule struct {
    Pattern string   // Glob pattern (e.g., "**/*.csv", "public/**")
    Access  *Access  // Permission definitions
    Limits  *Limits  // Resource limits
}
```

#### Access
Permission sets for different operations:

```go
type Access struct {
    Admin mapset.Set[string]  // Admin users (can modify ACLs)
    Read  mapset.Set[string]  // Read permission users
    Write mapset.Set[string]  // Write permission users (create/update/delete)
}

// Special values in sets:
// "*" = all users (public access)
// "alice@example.com" = specific user email
```

#### AccessLevel
Bitwise flags for permission levels:

```go
type AccessLevel uint8

const (
    AccessRead   AccessLevel = 1  // 0001 - View files
    AccessCreate AccessLevel = 2  // 0010 - Create new files
    AccessWrite  AccessLevel = 4  // 0100 - Modify/delete files
    AccessAdmin  AccessLevel = 8  // 1000 - Modify ACL files
)
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

## Detailed Execution Flows

### Read Operation Flow

When a user attempts to read a file (e.g., `bob` reading `/alice/public/data.csv`):

```
1. REQUEST RECEIVED
   └─> Path: "/alice/public/data.csv"
   └─> User: "bob@example.com"
   └─> Operation: AccessRead (level = 1)

2. OWNER CHECK
   └─> Extract owner from path: "alice"
   └─> Is bob == alice? NO → Continue to ACL check

3. CACHE LOOKUP
   └─> Cache key: "bob@example.com:/alice/public/data.csv:1"
   └─> Cache hit? NO → Continue to tree traversal

4. TREE TRAVERSAL
   └─> Normalize path: "alice/public/data.csv"
   └─> Split segments: ["alice", "public", "data.csv"]
   └─> Traverse tree:
       root → alice → public
   └─> Node found at: "alice/public" (has rules)

5. RULE EVALUATION
   └─> Rules at node (sorted by specificity):
       [0] pattern: "**/*.csv", access: {read: ["*"], write: ["alice"]}
       [1] pattern: "**", access: {read: ["bob@example.com", "carol@example.com"]}
   └─> Test pattern "alice/public/**/*.csv" against "alice/public/data.csv"
       → MATCH! Check access...
   └─> Is "bob@example.com" in read set ["*"]? 
       → YES ("*" = everyone)

6. CACHE UPDATE & RETURN
   └─> Store in cache: {"bob@example.com:/alice/public/data.csv:1" → ALLOW}
   └─> Return: ALLOW
```

### Write Operation Flow

When a user attempts to write a file (e.g., `carol` creating `/alice/shared/report.txt`):

```
1. REQUEST RECEIVED
   └─> Path: "/alice/shared/report.txt"
   └─> User: "carol@example.com"
   └─> Operation: AccessCreate (level = 2)
   └─> File size: 1024 bytes

2. OWNER CHECK
   └─> Extract owner: "alice"
   └─> Is carol == alice? NO → Continue

3. ACL FILE CHECK
   └─> Is path "syft.pub.yaml"? NO → Continue with normal check
   
4. TREE LOOKUP (with caching)
   └─> Cache miss → Tree traversal
   └─> Path segments: ["alice", "shared", "report.txt"]
   └─> Walk tree: root → alice → shared
   └─> No rules at "alice/shared"
   └─> Backtrack to "alice" (has rules)

5. RULE MATCHING
   └─> Rules at "alice" node:
       [0] pattern: "shared/**", access: {write: ["carol@example.com", "dave@example.com"]}
       [1] pattern: "**", access: {read: [], write: []}
   └─> Test "alice/shared/**" against "alice/shared/report.txt"
       → MATCH!
   └─> Check write access: "carol@example.com" ∈ ["carol@example.com", "dave@example.com"]
       → YES

6. LIMITS CHECK (if applicable)
   └─> Rule limits: {maxFileSize: 10485760, maxFiles: 100}
   └─> File size 1024 < 10485760? YES
   └─> File count for carol in /alice/shared: 5 < 100? YES

7. PERMISSION GRANTED
   └─> Cache result
   └─> Return: ALLOW
```

### ACL File Modification Flow

When modifying ACL files, special elevation occurs:

```
1. REQUEST: Write to "/alice/projects/syft.pub.yaml"
   └─> User: "alice@example.com"
   └─> Operation: AccessWrite (level = 4)

2. ACL FILE DETECTION
   └─> Is filename "syft.pub.yaml"? YES
   └─> ELEVATE to AccessAdmin (level = 8)

3. PERMISSION CHECK
   └─> Owner check: alice == alice? YES
   └─> Return: ALLOW (owner has admin rights)

4. POST-MODIFICATION UPDATES
   └─> Parse new ACL content
   └─> Update tree structure:
       - Remove old rules from node
       - Add new rules to node
       - Sort rules by specificity
       - Increment node version
   └─> Invalidate cache entries with prefix "/alice/projects/"
```

## Tree-Based Lookup Algorithm

### Rule Specificity Calculation

Rules are sorted by a specificity score to ensure most specific patterns match first:

```go
// Example calculation for pattern "public/**/*.csv"
baseScore := len("public/**/*.csv")*2 + strings.Count("public/**/*.csv", "/")*10
          = 15*2 + 2*10 
          = 30 + 20 
          = 50


// Apply wildcard penalties
score := 50
score -= 10  // for first '*' in "**"
score -= 10  // for second '*' in "**"
score -= 10  // for '*' in "*.csv"
// Final score: 20

// Comparison with other patterns:
"public/data.csv"    → score: 34 (most specific)
"public/*.csv"       → score: 14
"public/**/*.csv"    → score: 20
"**"                 → score: -100 (least specific)
```

### Complete Example: Complex Permission Check

Consider this directory structure and ACL configuration:

```
alice/
├── syft.pub.yaml       # Root ACL
│   terminal: false
│   rules:
│     - pattern: "**/*.csv"
│       access: {read: ["bob@example.com", "carol@example.com"]}  # Individual emails
│     - pattern: "**"
│       access: {read: []}
│
├── public/
│   ├── syft.pub.yaml
│   │   terminal: false
│   │   rules:
│   │     - pattern: "**"
│   │       access: {read: ["*"]}  # Public access
│   └── data.csv       # Target file
│
└── private/
    └── syft.pub.yaml   # Private ACL (terminal)
        terminal: true
        rules:
          - pattern: "**"
            access: {read: [], write: []}
```

When `bob@example.com` tries to read `/alice/public/data.csv`:

```
STEP 0: Owner Check
- User: "bob@example.com"
- Path: "alice/public/data.csv"
- Is bob owner of alice? NO → Continue to ACL check

STEP 1: Cache Lookup
- Check cache for "bob@example.com:/alice/public/data.csv:1"
- Cache miss → Continue to tree lookup

STEP 2: Tree Lookup
- Path: "alice/public/data.csv"
- Traverse: root → alice → public
- Found node with rules at: "alice/public"
- Rules to evaluate: [ACLRule{fullPattern: "alice/public/**", ...}]

STEP 3: Pattern Matching
- Test "alice/public/**" against "alice/public/data.csv"
  → MATCH! 

STEP 4: Access Check
- Check if "bob@example.com" has read access
- Rule has read: ["*"] → ALLOW

STEP 5: Cache Update
- Store result in cache
- Return: ALLOW

RESULT: Access granted (stopped at first matching rule)
```

### Data Structure States During Execution

Here's how the data structures look during a permission check:

```go
// ACLNode at "alice/public" after loading rules:
&ACLNode{
    mu:       sync.RWMutex{},
    path:     "alice/public",
    owner:    "alice",
    terminal: false,
    version:  3,
    depth:    2,
    rules: []*ACLRule{
        {
            fullPattern: "alice/public/**",
            rule: &aclspec.Rule{
                Pattern: "**",
                Access: &aclspec.Access{
                    Read:  mapset.NewSet("*"),
                    Write: mapset.NewSet[string](),
                    Admin: mapset.NewSet[string](),
                },
                Limits: &aclspec.Limits{},
            },
            node: /* reference to this node */,
        },
    },
    children: map[string]*ACLNode{},  // May be empty initially
}

// Cache entry after successful lookup:
cache.index["alice/public/data.csv"] = &ACLRule{
    fullPattern: "alice/public/**",
    rule: &aclspec.Rule{...},
    node: /* reference to alice/public node */,
}
```

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
      read: ["bob@example.com", "carol@example.com"]  # Individual team members
      write: ["alice@example.com"]  # Owner only

  - pattern: "**"           # Everything else is private
    access:
      read: []
      write: []
```

**Security Benefits:**
- **No Subdirectory Overrides**: Even if someone creates a `syft.pub.yaml` file in a subdirectory, it won't be evaluated
- **Exploit Prevention**: Malicious programs cannot escalate permissions by creating ACL files in subdirectories
- **Centralized Control**: All permissions are controlled from one location at the top level
- **Audit Trail**: Easy to review all permissions in a single file

**Important Implementation Details:**
- Child ACL files can still exist in the tree structure (for performance), but they are completely ignored during permission checks
- The system stops looking for ACL rules as soon as it encounters a terminal node
- This creates a hard security boundary that cannot be bypassed by subdirectory ACL files

## Concrete Examples with Data Structure Values

### Example 1: Multi-User Collaboration Setup

Consider this YAML configuration and resulting data structures:

```yaml
# /alice/projects/syft.pub.yaml
terminal: false
rules:
  - pattern: "docs/**/*.md"
    access:
      read: ["*"]
      write: ["alice@example.com", "bob@example.com"]
  - pattern: "src/**"
    access:
      read: ["alice@example.com", "bob@example.com", "carol@example.com"]
      write: ["alice@example.com"]
  - pattern: "**"
    access:
      read: ["alice@example.com"]
```

**Resulting ACLNode Structure:**
```go
&ACLNode{
    path:     "alice/projects",
    owner:    "alice",
    terminal: false,
    version:  5,
    depth:    2,
    rules: []*ACLRule{
        {
            fullPattern: "alice/projects/docs/**/*.md",
            rule: &Rule{
                Pattern: "docs/**/*.md",
                Access: &Access{
                    Read:  mapset.NewSet("*"),  // Public read
                    Write: mapset.NewSet("alice@example.com", "bob@example.com"),
                    Admin: mapset.NewSet[string](),
                },
            },
        },
        {
            fullPattern: "alice/projects/src/**",
            rule: &Rule{
                Pattern: "src/**",
                Access: &Access{
                    Read:  mapset.NewSet("alice@example.com", "bob@example.com", "carol@example.com"),
                    Write: mapset.NewSet("alice@example.com"),
                    Admin: mapset.NewSet[string](),
                },
            },
        },
        {
            fullPattern: "alice/projects/**",
            rule: &Rule{
                Pattern: "**",
                Access: &Access{
                    Read:  mapset.NewSet("alice@example.com"),
                    Write: mapset.NewSet[string](),
                    Admin: mapset.NewSet[string](),
                },
            },
        },
    },
}
```

### Example 2: Terminal Node with File Size Limits

```yaml
# /alice/uploads/syft.pub.yaml
terminal: true  # No subdirectory ACLs will be processed
rules:
  - pattern: "temp/**"
    access:
      write: ["*"]
      read: ["alice@example.com"]
    limits:
      maxFileSize: 5242880  # 5MB
      allowDirs: false      # No directories allowed
      allowSymlinks: false  # No symlinks allowed
  - pattern: "**"
    access:
      read: []
      write: []
```

**When user "eve@example.com" uploads to "/alice/uploads/temp/data.json" (2MB):**

```go
// 1. Tree traversal finds node:
node := &ACLNode{
    path:     "alice/uploads",
    terminal: true,  // STOP HERE - don't look deeper
    rules: [/* rules array */],
}

// 2. First matching rule:
matchedRule := &ACLRule{
    fullPattern: "alice/uploads/temp/**",
    rule: &Rule{
        Pattern: "temp/**",
        Access: &Access{
            Write: mapset.NewSet("*"),  // Everyone can write
        },
        Limits: &Limits{
            MaxFileSize: 5242880,
            AllowDirs: false,
            AllowSymlinks: false,
        },
    },
}

// 3. Permission check (actual implementation):
everyoneWrite := matchedRule.rule.Access.Write.Contains("*")  // true
isWriter := everyoneWrite  // true (since eve@example.com is not admin)

// 4. File size check:
fileSize := 2097152  // 2MB
sizeOK := fileSize <= matchedRule.rule.Limits.MaxFileSize  // true

// 5. Directory check:
isDir := false  // data.json is a file
dirOK := matchedRule.rule.Limits.AllowDirs || !isDir  // true

// Result: ALLOW
```

### Example 3: Owner-Based Access Control

```yaml
# /alice/shared/syft.pub.yaml
rules:
  - pattern: "team/**"
    access:
      read: ["alice@example.com", "bob@example.com", "carol@example.com"]
      write: ["alice@example.com"]
  - pattern: "public/**"
    access:
      read: ["*"]
      write: ["alice@example.com"]
```

**When user "bob@example.com" tries to read "/alice/shared/team/report.pdf":**

```go
// 1. Find matching rule:
matchedRule := &ACLRule{
    fullPattern: "alice/shared/team/**",
    rule: &Rule{
        Pattern: "team/**",
        Access: &Access{
            Read: mapset.NewSet("alice@example.com", "bob@example.com", "carol@example.com"),
            Write: mapset.NewSet("alice@example.com"),
        },
    },
}

// 2. Permission check (actual implementation):
user := &User{ID: "bob@example.com"}
level := AccessRead

// Check if user is owner (automatic full access):
isOwner := matchedRule.Owner() == user.ID  // false (owner is "alice")

// Check read permissions:
everyoneRead := matchedRule.rule.Access.Read.Contains("*")  // false
userRead := matchedRule.rule.Access.Read.Contains(user.ID)  // true
isReader := everyoneRead || userRead  // true

// Result: ALLOW (bob@example.com has read access)
```

**When user "eve@example.com" tries to read the same file:**

```go
// Same rule, different user:
user := &User{ID: "eve@example.com"}

// Permission check:
isOwner := matchedRule.Owner() == user.ID  // false
everyoneRead := matchedRule.rule.Access.Read.Contains("*")  // false
userRead := matchedRule.rule.Access.Read.Contains(user.ID)  // false
isReader := everyoneRead || userRead  // false

// Result: DENY (eve@example.com not in read list)
```

## Caching Strategy

### Simple Path-Based Cache Structure

The ACL system uses a straightforward path-based cache to store effective rules for file access:

```go
// ACLCache stores the effective ACL rule for a given path.
type ACLCache struct {
    index map[string]*ACLRule // Normalized path -> effective ACLRule
    mu    sync.RWMutex        // Thread-safe access
}

// Cache Entry (implicit)
type CacheEntry struct {
    path: string,      // e.g., "alice/projects/src/main.go"
    rule: *ACLRule,    // The effective rule for this path
}
```

### Cache Operations

```go
// Get: O(1) cache lookup
cachedRule := cache.Get("alice/projects/src/main.go")
if cachedRule != nil {
    return cachedRule  // Cache hit
}

// Set: O(1) cache storage
cache.Set("alice/projects/src/main.go", effectiveRule)

// DeletePrefix: O(n) where n = number of cached entries
deleted := cache.DeletePrefix("alice/projects")  // Clears all entries under this path
```

### Cache Invalidation Example

When ACL file `/alice/projects/syft.pub.yaml` is updated:

```go
// 1. Update triggers invalidation
path := "alice/projects"

// 2. Tree is updated and node version incremented
node, err := tree.AddRuleSet(ruleSet)  // Updates tree, increments version
if err != nil {
    return err
}

// 3. Clear prefix-based cache entries
deleted := cache.DeletePrefix(path)  // Clears all entries under "alice/projects"

// Example cleared entries:
// - "alice/projects/src/main.go"
// - "alice/projects/docs/readme.md"
// - "alice/projects/data.csv"
// - "alice/projects/subdir/file.txt"

slog.Debug("updated rule set", 
    "path", node.path, 
    "version", node.version, 
    "cache.deleted", deleted, 
    "cache.count", cache.Count())
```

### Cache Warming

On service startup, the cache is warmed by checking access for all files:

```go
// Warm up the ACL cache
for blob := range blobIndex.Iter() {
    if err := service.CanAccess(
        &User{ID: "*"},  // Check with "everyone" user
        &File{Path: blob.Key},
        AccessRead,
    ); err != nil && errors.Is(err, ErrNoRule) {
        slog.Warn("acl cache warm error", "path", blob.Key, "error", err)
    }
}
```

### Blob Change Handling

The cache is automatically updated when files are deleted:

```go
func (s *ACLService) onBlobChange(key string, eventType blob.BlobEventType) {
    if eventType == blob.BlobEventDelete {
        // Clean up cache entry for the deleted file
        s.cache.Delete(key)
        slog.Debug("acl cache removed", "key", key, "cache.count", s.cache.Count())
    }
}
```

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
  AllowDirs     bool   // Allow directory creation, default: true
  AllowSymlinks bool   // Allow symbolic links, default: false
}
```

**Note:** `MaxFiles` limit is defined in the struct but not currently implemented in the codebase.

#### Example: Storage Quotas

```yaml
# Limit uploads from external contributors
terminal: false
rules:
  - pattern: "contributions/**"
    access:
      write: ["*"]  # Anyone can contribute
      read: ["alice@example.com", "bob@example.com"]
    limits:
      maxFileSize: 10485760  # 10MB per file
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
      read: ["alice@example.com"]
    limits:
      maxFileSize: 5242880  # 5MB max
      allowDirs: false      # No subdirectories
      allowSymlinks: false
```

#### Limits Enforcement

- Limits are checked during write operations (create/update)
- File size is validated before accepting uploads
- Directory creation and symlinks can be controlled
- Default limits (0) mean no restriction

### Path Depth Limits

- Maximum tree depth: 255 levels
- Prevents deep nesting attacks
- Uses uint8 storage for depth values
- Error: `ErrMaxDepthExceeded` when limit is exceeded

## Performance Optimizations

### Parallel Processing

- Concurrent ACL file fetching (16 workers)
- Non-blocking cache operations with `sync.RWMutex`
- Efficient tree traversal O(d) where d = path depth

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
- `ErrNoRuleSet`: No ruleset found for path
- `ErrMaxDepthExceeded`: Path exceeds 255 levels
- `ErrInvalidRuleset`: Malformed YAML or missing required fields
- `ErrNoAdminAccess`: User lacks admin permissions
- `ErrNoWriteAccess`: User lacks write permissions
- `ErrNoReadAccess`: User lacks read permissions
- `ErrFileSizeExceeded`: File size exceeds limits
- `ErrDirsNotAllowed`: Directory creation not allowed
- `ErrSymlinksNotAllowed`: Symbolic links not allowed

### Graceful Degradation

- Missing ACL files result in no rules (access denied)
- Invalid rules are logged but don't crash the system
- Cache misses fall back to tree evaluation

## ACL File Management

### Automatic Setup During Workspace Initialization

The client automatically creates default ACL rules for **specific folders** during workspace setup:

```
datasites/
└── alice/                      # User's datasite root
    ├── syft.pub.yaml          # Root ACL (created automatically)
    └── public/                # Public folder (created automatically)
        └── syft.pub.yaml      # Public ACL (created automatically)
```

**Automatic ACL Creation:**
- **Root User Directory**: Private access (no access for anyone except owner)
- **Public Directory**: Public read access (readable by everyone)

**When This Happens:**
- During workspace setup (`w.Setup()`)
- Only if ACL files don't already exist
- Creates exactly two default ACL files

### Manual Setup Required for New Folders

For any **new folders created by users**, ACL rules must be created manually. The automatic creation only applies to the initial workspace setup.

**Example:**
```
alice/
├── syft.pub.yaml              # ✅ Created automatically (private)
├── public/
│   └── syft.pub.yaml         # ✅ Created automatically (public read)
├── projects/                  # ❌ User-created folder
│   └── syft.pub.yaml         # ❌ Must be created manually
└── shared/                    # ❌ User-created folder
    └── syft.pub.yaml         # ❌ Must be created manually
```

### Recommended Root ACL

The automatically created root ACL uses private access by default:

```yaml
# datasites/alice/syft.pub.yaml (created automatically)
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

### Recommended Public Folder

The automatically created public ACL provides global read access:

```yaml
# datasites/alice/public/syft.pub.yaml (created automatically)
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
├── syft.pub.yaml              # Private root (auto-created)
├── public/                    # Public datasets (auto-created)
│   ├── syft.pub.yaml         # Read: everyone (auto-created)
│   └── datasets/
├── projects/                  # Collaborative projects (user-created)
│   ├── research/
│   │   └── syft.pub.yaml     # Custom team permissions (manual)
│   └── development/
│       └── syft.pub.yaml     # Different team permissions (manual)
└── private/                   # Sensitive data (user-created)
    └── syft.pub.yaml         # Terminal: true, strict access (manual)
```

### Security Recommendations

1. **Start Private**: Use private root, explicitly grant access
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
