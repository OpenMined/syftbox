# Access Control List (ACL) System

## Overview

The SyftBox ACL system provides fine-grained access control for files and directories within datasites. It uses a hierarchical rule-based system with YAML configuration files (`syft.pub.yaml`) to define permissions across read, write, and admin operations. The system supports advanced features including template patterns, dynamic user resolution, and efficient caching.

## Architecture

### Core Components

- **ACLService**: Main service managing ACL rules and access validation
- **ACLTree**: N-ary tree structure for efficient hierarchical ACL storage and lookup
- **ACLCache**: High-performance LRU cache with TTL for rapid access control decisions
- **ACLSpec**: YAML-based ACL specification and parsing system
- **RuleSet**: Collection of rules applied to a specific path
- **Access**: Permission definitions for admin, read, and write operations
- **Matcher**: Pattern matching system supporting glob patterns and templates

### System Design

The ACL system follows a hierarchical, path-based approach with advanced pattern matching:

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
        │
    ┌───┴───┐
    │Rules  │
    │Matcher│
    └───────┘
```

## ACL File Format

### File Location

ACL rules are defined in `syft.pub.yaml` files placed in directories throughout the datasite structure. Each file controls access to its directory and subdirectories.

### YAML Structure

```yaml
# Terminal flag - stops inheritance from parent directories
terminal: false

# Rules array - evaluated in order of specificity
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
- `USER`: Dynamic token that resolves to the requesting user's ID

### Domain-Based Access Control

The ACL system supports glob pattern matching in access lists, allowing you to grant permissions to all users with specific domains:

```yaml
rules:
  - pattern: "company_docs/**"
    access:
      read: ["*@company.com"]      # All users with @company.com domain
      write: ["*@company.com"]     # All users with @company.com domain
  
  - pattern: "admin_docs/**"
    access:
      read: ["admin@company.com", "ceo@company.com"]
      write: ["admin@company.com"]
  
  - pattern: "public/**"
    access:
      read: ["*"]                  # Everyone
      write: ["*@company.com"]     # Only company users can write
```

**Supported Domain Patterns:**
- `*@company.com` - Any user with @company.com domain
- `*@*.company.com` - Any user with any subdomain of company.com
- `admin@*.company.com` - Admin users in any subdomain
- `*@engineering.company.com` - Users in engineering subdomain
- `*@*.com` - Any user with any .com domain (use carefully!)

## Data Structures

### Core Service Structure

#### ACLService
The main service orchestrating all ACL operations:

```go
type ACLService struct {
    blob  blob.Service      // Interface to fetch ACL files
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
│       └── rules: [ACLRule{fullPattern: "alice/private/**", rule: {pattern: "**", access: {read: [], write: []}}}]
```

#### ACLRule
Compiled rule with resolved patterns and matcher:

```go
type ACLRule struct {
    fullPattern string    // Complete pattern (e.g., "alice/public/**/*.csv")
    rule        *Rule     // Original rule from YAML
    node        *ACLNode  // Parent node reference
    matcher     Matcher   // Pattern matching engine
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
    Pattern string  `yaml:"pattern"`   // Glob pattern (e.g., "**/*.csv", "public/**")
    Access  *Access `yaml:"access"`  // Permission definitions
    Limits  *Limits  `yaml:"-"`  // Resource Limitation - Current disabled
    // Limits field is excluded from YAML serialization (yaml:"-")
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
// "USER" = dynamic token resolved to requesting user
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

### Pattern Matching System

#### Matcher Interface
The ACL system uses a pluggable matcher interface for different pattern types:

```go
type Matcher interface {
    Match(path string, ctx MatchContext) (bool, error)
    Type() MatcherType
}

type MatchContext any // Context for template resolution (typically *User)
```

#### Matcher Types

```go
type MatcherType int

const (
    MatcherTypeExact MatcherType = iota    // Direct string comparison
    MatcherTypeGlob                        // Standard glob patterns
    MatcherTypeTemplate                    // Dynamic templates
)
```

#### Exact Matcher
For direct path matching:

```go
type ExactMatcher struct {
    Value string  // Exact path to match
}

// Example: pattern "alice/public/file.txt"
// Matches: "alice/public/file.txt"
// Does not match: "alice/public/other.txt"
```

#### Glob Matcher
For standard glob pattern matching:

```go
type GlobMatcher struct {
    pattern string  // Glob pattern (e.g., "*.txt", "**/*.csv")
}

// Examples:
// "*.txt" matches any .txt file in current directory
// "**/*.csv" matches any .csv file in any subdirectory
// "public/**" matches anything under public/
```

#### Template Matcher
For dynamic pattern resolution:

```go
type TemplateMatcher struct {
    tpl *template.Template  // Compiled Go template
}

// Example: pattern "user_{{.UserEmail}}/**"
// Resolves to: "user_alice@example.com/**" for user alice@example.com
```

#### Template Data
Available variables for template resolution:

```go
type templateData struct {
    UserEmail string  // User's email address
    UserHash  string  // SHA256 hash of user email (8 chars)
    Year      string  // Current year (4 digits)
    Month     string  // Current month (2 digits)
    Date      string  // Current day (2 digits)
}
```

#### Template Functions
Built-in functions for template manipulation:

```go
var tplFuncMap = template.FuncMap{
    "sha2": func(s string, n ...uint8) string {
        // Returns SHA256 hash, optionally truncated
    },
    "upper": strings.ToUpper,  // Convert to uppercase
    "lower": strings.ToLower,  // Convert to lowercase
}
```

## Access Levels

The system defines four access levels with increasing privileges:

1. **AccessRead** (Level 1): Read file contents
2. **AccessCreate** (Level 2): Create new files
3. **AccessWrite** (Level 4): Modify/delete files
4. **AccessAdmin** (Level 8): Full control including ACL modifications

### Permission Hierarchy

- **Read**: View file contents and metadata
- **Write**: Includes Create, Update, Delete operations
- **Admin**: Full control over files and ACL rules

## Advanced Pattern Matching

### Template Patterns

The ACL system supports dynamic template patterns using Go's text/template syntax:

```yaml
rules:
  - pattern: "private_{{.UserEmail}}/**"
    access:
      read: ["USER"]  # Only the user whose email matches the pattern
      write: ["USER"]
  
  - pattern: "hash_{{.UserHash}}/**"
    access:
      read: ["USER"]
  
  - pattern: "year_{{.Year}}/month_{{.Month}}/**"
    access:
      read: ["*"]
```

#### Available Template Variables

**`TokenUser='USER'` is applicable to ALL template variables** and can be used in access lists with any template pattern:

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.UserEmail}}` | User's email address | `alice@example.com` |
| `{{.UserHash}}` | SHA256 hash of user email (8 chars) | `a1b2c3d4` |
| `{{.Year}}` | Current year (4 digits) | `2024` |
| `{{.Month}}` | Current month (2 digits) | `12` |
| `{{.Date}}` | Current day (2 digits) | `25` |

#### Template Functions

| Function | Description | Example |
|----------|-------------|---------|
| `{{upper .UserEmail}}` | Convert to uppercase | `ALICE@EXAMPLE.COM` |
| `{{lower .UserEmail}}` | Convert to lowercase | `alice@example.com` |
| `{{sha2 .UserEmail}}` | Full SHA256 hash | `a1b2c3d4e5f6...` |
| `{{sha2 .UserEmail 8}}` | Truncated SHA256 hash | `a1b2c3d4` |

### USER Token Resolution

The `USER` token dynamically resolves to the requesting user's ID during access evaluation. **Important**: The behavior of `USER` token depends on the pattern context:

#### **With Template Patterns (e.g., `{UserEmail}/**`)**
- `TokenUser` provides **user segregation** and **individual access control**
- Example: `"private_{{.UserEmail}}/**"` with `read: ["USER"]`
- This creates **user-specific private spaces** where each user can only access their own directory
- `USER` token resolves to the requesting user's email, ensuring isolation

#### **With Universal Patterns (e.g., `**`)**
- `TokenUser` becomes **equivalent to TokenEveryone** (`*`)
- Example: `"**"` with `read: ["USER"]`
- This grants access to **any authenticated user**, not just the requesting user
- The `**` pattern matches everything, so `USER` effectively becomes public access

```yaml
rules:
  - pattern: "personal/**"
    access:
      read: ["USER"]      # Resolves to requesting user's email
      write: ["USER"]
  
  - pattern: "shared/**"
    access:
      read: ["USER", "bob@example.com"]  # USER + specific user
      write: ["alice@example.com"]
```

**Example Resolution:**
- User `carol@example.com` requests access to `personal/file.txt`
- `USER` token resolves to `carol@example.com`
- Access check: `carol@example.com` ∈ `["carol@example.com"]` → **ALLOW**

#### Implementation Details

The USER token resolution happens during the `CheckAccess` method:

```go
func (r *ACLRule) resolveAccessList(accessList mapset.Set[string], userID string) mapset.Set[string] {
    // If USER token is in the access list, replace it with the actual user ID
    if accessList.Contains(aclspec.TokenUser) {
        clone := mapset.NewSet(accessList.ToSlice()...)
        clone.Add(userID)
        clone.Remove(aclspec.TokenUser)
        return clone
    }
    return accessList
}
```

**Key Features:**
- **Dynamic Resolution**: USER token is resolved at access time, not at rule compilation
- **Non-Destructive**: Original rule is not modified, resolution happens per request
- **Efficient**: Uses set operations for fast token replacement
- **Flexible**: Can be combined with other user IDs in the same access list

**Security Implications:**
- **Template Patterns**: Provide **true user isolation** and **multi-tenancy**
- **Universal Patterns**: `USER` token provides **authentication requirement** but not **authorization isolation**
- **Best Practice**: Use template patterns for user-specific resources, avoid `USER` with `**` for sensitive data

### Pattern Matching Types

The system supports three types of pattern matching:

1. **Exact Matcher**: Direct string comparison
2. **Glob Matcher**: Standard glob patterns (`*`, `**`, `?`, `[abc]`)
3. **Template Matcher**: Dynamic templates with variable resolution

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
   └─> Cache key: "alice/public/data.csv:bob@example.com:1"
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
   └─> Store in cache: {"alice/public/data.csv:bob@example.com:1" → ALLOW}
   └─> Return: ALLOW
```

### Template Pattern Resolution Flow

When a user accesses a template-based pattern:

```
1. REQUEST: "alice@example.com/private_bob@example.com/file.txt"
   └─> User: "bob@example.com"
   └─> Pattern: "private_{{.UserEmail}}/**"

2. TEMPLATE RESOLUTION
   └─> Template: "private_{{.UserEmail}}/**"
   └─> Variables: {UserEmail: "bob@example.com"}
   └─> Resolved: "private_bob@example.com/**"

3. PATTERN MATCHING
   └─> Test: "private_bob@example.com/**" matches "alice@example.com/private_bob@example.com/file.txt"
   └─> Result: MATCH

4. USER TOKEN RESOLUTION
   └─> Access: {read: ["USER"]}
   └─> Resolved: {read: ["bob@example.com"]}
   └─> Check: "bob@example.com" ∈ ["bob@example.com"] → ALLOW
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

6. ACCESS CHECK
   └─> Rule access: {write: ["carol@example.com", "dave@example.com"]}
   └─> Check write access: "carol@example.com" ∈ ["carol@example.com", "dave@example.com"]
   └─> YES

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
score -= 20  // for "**" (leading wildcard penalty)
score -= 10  // for '*' in "*.csv"
// Final score: 20

// Template patterns get bonus points
if hasTemplatePattern(pattern) {
    score += 50
}

// Comparison with other patterns:
"public/*.txt"       → score: 24
"public/**/*.csv"    → score: 20
"**"                 → score: -100 (least specific)
```

**Scoring Formula Breakdown:**
- **Base Score**: `2 × Length + 10 × DirectoryCount`
- **Template Bonus**: `+50` for any pattern containing `{{...}}`
- **Wildcard Penalties**: 
  - `-20` for leading wildcards (`*`, `**`)
  - `-10` for non-leading wildcards (`*`)
  - `-2` for other wildcard characters (`?`, `[abc]`, `{...}`)
- **Special Cases**: `"**" = -100`, `"**/*" = -99`

**Detailed Scoring Examples:**

```go
// Simple file pattern
"file.txt" → 2(8) + 10(0) = 16

// Directory with wildcard
"public/*.txt" → 2(12) + 10(1) - 10 = 24

// Deep wildcard pattern
"public/**/*.csv" → 2(15) + 10(2) - 20 - 10 = 20

// Template pattern
"{{.UserEmail}}/*" → 2(18) + 10(1) + 50 = 78

// Complex nested template
"alice@email.com/{{.UserEmail}}/ben@email.com/{{.UserHash}}/*" 
→ 2(60) + 10(3) + 50 = 192

// Universal patterns (special cases)
"**" → -100
"**/*" → -99
```

**How Scoring Ensures Proper Rule Ordering:**

1. **Template Patterns Get Highest Priority**: The +50 bonus ensures user-specific patterns are evaluated first
2. **Specific Patterns Override General Ones**: `"public/*.txt"` (24) > `"public/**/*.csv"` (20) > `"**"` (-100)
3. **Leading Wildcards Are Heavily Penalized**: Patterns starting with `*` or `**` get -20 penalty
4. **Directory Depth Matters**: More specific paths get higher scores due to directory separator bonuses

**Result**: Rules are automatically sorted from most specific to least specific, ensuring security and preventing accidental permission overrides.

### Complete Example: Complex Permission Check

Consider this directory structure and ACL configuration:

```
alice/
├── syft.pub.yaml       # Root ACL
│   terminal: false
│   rules:
│     - pattern: "**/*.csv"
│       access: {read: ["bob@example.com", "carol@example.com"]}
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
- Check cache for "alice/public/data.csv:bob@example.com:1"
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
                // Limits field is excluded from YAML serialization
            },
            node: /* reference to this node */,
            matcher: &GlobMatcher{pattern: "alice/public/**"},
        },
    },
    children: map[string]*ACLNode{},  // May be empty initially
}

// Cache entry after successful lookup:
cache.index["alice/public/data.csv:bob@example.com:1"] = true
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
            matcher: &GlobMatcher{pattern: "alice/projects/docs/**/*.md"},
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
            matcher: &GlobMatcher{pattern: "alice/projects/src/**"},
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
            matcher: &GlobMatcher{pattern: "alice/projects/**"},
        },
    },
}
```

### Example 2: Template-Based User Isolation

```yaml
# /alice/uploads/syft.pub.yaml
terminal: true
rules:
  - pattern: "user_{{.UserEmail}}/**"
    access:
      read: ["USER"]
      write: ["USER"]
  - pattern: "public/**"
    access:
      read: ["*"]
      write: ["alice@example.com"]
  - pattern: "**"
    access:
      read: []
      write: []
```

**When user "bob@example.com" uploads to "/alice/uploads/user_bob@example.com/data.json" (2MB):**

```go
// 1. Template resolution:
template: "user_{{.UserEmail}}/**"
variables: {UserEmail: "bob@example.com"}
resolved: "user_bob@example.com/**"

// 2. Pattern matching:
pattern: "alice/uploads/user_bob@example.com/**"
path: "alice/uploads/user_bob@example.com/data.json"
result: MATCH

// 3. USER token resolution:
access: {read: ["USER"], write: ["USER"]}
resolved: {read: ["bob@example.com"], write: ["bob@example.com"]}

// 4. Permission check:
user: "bob@example.com"
isWriter := access.Write.Contains(user.ID)  // true

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
    matcher: &GlobMatcher{pattern: "alice/shared/team/**"},
}

// 2. Permission check:
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

### LRU Cache with TTL

The ACL system uses a sophisticated LRU cache with time-based expiration:

```go
// ACLCache stores access decisions with automatic expiration
type ACLCache struct {
    index *expirable.LRU[aclCacheKey, bool]  // LRU with TTL
}

// Cache configuration
const (
    aclCacheTTL        = time.Hour * 1        // 1 hour TTL
    aclAccessCacheSize = 100_000              // 100K entries max
)
```

### Cache Key Structure

Cache keys include user context for proper isolation:

```go
// Cache key format: "path:userID:accessLevel"
func newCacheKeyByUser(req *ACLRequest) aclCacheKey {
    return aclCacheKey(fmt.Sprintf("%s:%s:%d", req.Path, req.User.ID, req.Level))
}

// Examples:
// "alice/public/data.csv:bob@example.com:1"  // Read access
// "alice/public/data.csv:carol@example.com:2" // Create access
// "alice/private/file.txt:bob@example.com:1"  // Different path
```

### Cache Operations

```go
// Get: O(1) cache lookup with TTL check
cachedResult, exists := cache.Get(req)
if exists {
    return cachedResult  // Cache hit (or expired entry)
}

// Set: O(1) cache storage with automatic TTL
cache.Set(req, canAccess)

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
// - "alice/projects/src/main.go:bob@example.com:1"
// - "alice/projects/docs/readme.md:carol@example.com:1"
// - "alice/projects/data.csv:alice@example.com:4"

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
    if eventType == blob.BlobEventDelete && !aclspec.IsACLFile(key) {
        // Clean up cache entry for the deleted file
        deleted := s.cache.Delete(key)
        slog.Debug("acl cache removed", "key", key, "deleted", deleted, "cache.count", s.cache.Count())
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

### Template Security

- Template variables are sanitized and validated
- User input is not directly interpolated into templates
- Template functions are limited to safe operations

### File Limits and Extended Features

#### Limits Configuration

The ACL system supports resource limits to prevent abuse and manage storage efficiently, but these are **not configurable through YAML** due to the `yaml:"-"` tag:

```go
type Limits struct {

  MaxFileSize   int64 `yaml:"maxFileSize,omitempty"` // Maximum file size in bytes, default: 0 no limit
  AllowDirs     bool   `yaml:"allowDirs,omitempty"`// Allow directory creation, default: true
  AllowSymlinks bool   `yaml:"allowSymlinks,omitempty"` // Allow symbolic links, default: false
  MaxFiles      uint32 `yaml:"maxFiles,omitempty"` //`MaxFiles` limit is defined in the struct but not currently implemented in the codebase
}
```

**Note:** 
- Limits are not configurable through YAML files and are excluded from YAML serialization
- Limits functionality is implemented in the code but uses hardcoded default values

#### Example: Storage Quotas

```yaml
# Limit uploads from external contributors
terminal: false
rules:
  - pattern: "contributions/**"
    access:
      write: ["*"]  # Anyone can contribute
      read: ["alice@example.com", "bob@example.com"]
```

#### Example: Restricted Upload Area

```yaml
# Public upload area with access control
terminal: true
rules:
  - pattern: "uploads/temp/**"
    access:
      write: ["*"]
      read: ["alice@example.com"]
```

#### Limits Enforcement

- Limits are checked during write operations (create/update)
- File size is validated before accepting uploads
- Directory creation and symlinks can be controlled
- Default limits (0) mean no restriction
- **Note:** Limits are currently hardcoded and not configurable through YAML files

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
- Object pooling for template data

### Lookup Performance

- O(d) tree traversal where d = path depth
- O(1) cache hit for repeated access checks
- O(r) rule evaluation where r = rules per node
- Template compilation cached per pattern

### Cache Performance

- LRU eviction prevents memory bloat
- TTL ensures fresh data
- Prefix-based invalidation minimizes cache misses
- User-specific keys prevent permission leakage

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
- `ErrInvalidAccessLevel`: Invalid access level specified

### Graceful Degradation

- Missing ACL files result in no rules (access denied)
- Invalid rules are logged but don't crash the system
- Cache misses fall back to tree evaluation
- Template errors fall back to exact matching

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

#### 2. **Template-Based User Isolation**
```yaml
# /alice/uploads/syft.pub.yaml
terminal: true
rules:
  - pattern: "user_{{.UserEmail}}/**"
    access:
      read: ["USER"]
      write: ["USER"]
  - pattern: "public/**"
    access:
      read: ["*"]
      write: ["alice@example.com"]
  - pattern: "**"
    access:
      read: []
      write: []
```

#### 3. **Time-Based Access Control**
```yaml
# /alice/archives/syft.pub.yaml
terminal: true
rules:
  - pattern: "{{.Year}}/{{.Month}}/**"
    access:
      read: ["*"]
      write: ["alice@example.com"]
  - pattern: "**"
    access:
      read: ["alice@example.com"]
      write: ["alice@example.com"]
```

### Security Recommendations

1. **Start Private**: Use private root, explicitly grant access
2. **Use Public Folder**: Keep public data in designated `public/` directory
3. **Terminal for Sensitive Data**: Use terminal nodes for high-security zones
4. **Regular Audits**: Review ACL files periodically
5. **Test Permissions**: Verify access before sharing sensitive data
6. **Template Security**: Validate template patterns carefully
7. **Limit Scope**: Use specific patterns rather than broad `**` rules

## Best Practices

### ACL File Placement

1. Place `syft.pub.yaml` at directory boundaries
2. Use terminal nodes to prevent inheritance
3. Keep rules simple and ordered by specificity

### Pattern Design

1. Most specific patterns first
2. Use `**` as catch-all default rule
3. Leverage glob patterns for file type control
4. Use templates for dynamic user isolation

### Security Guidelines

1. Principle of least privilege
2. Explicit deny over implicit allow
3. Regular audit of ACL configurations
4. Test access patterns before deployment
5. Validate template patterns for security

### Performance Guidelines

1. Use terminal nodes for large directory trees
2. Keep rule sets small and focused
3. Leverage caching for frequently accessed paths
4. Monitor cache hit rates and adjust TTL as needed

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

### Template System

- Dynamic pattern resolution
- User-specific access control
- Time-based permissions
- Hash-based user identification


### Best Practices for Advanced Permissions

1. **Template Security**
   - Always validate template patterns before deployment
   - Use specific patterns rather than broad wildcards
   - Test template resolution with various user inputs

2. **Time-Based Patterns**
   - Consider timezone implications for global organizations
   - Plan for year transitions and edge cases
   - Use consistent date formatting across patterns

3. **Domain-Based Access Control**
   - Use specific domain patterns (`*@company.com`) rather than broad ones (`*@*.com`)
   - Test domain patterns with various email formats
   - Consider subdomain organization for better access control
   - Document domain patterns clearly for maintenance

4. **Performance Considerations**
   - Limit the number of template rules per ACL file
   - Use terminal nodes for large directory trees
   - Monitor cache performance with template-heavy configurations
   - Domain pattern matching is efficient but should be used judiciously

5. **Security Considerations**
   - Avoid overly broad domain patterns that could grant unintended access
   - Regularly audit domain-based access rules
   - Consider the impact of domain changes on access control
   - Use specific subdomains for role-based access when possible

6. **Maintenance**
   - Document template patterns clearly
   - Regular review of time-based access rules
   - Automated testing of template resolution
   - Keep domain patterns updated with organizational changes

## Summary

The SyftBox ACL system provides a comprehensive, high-performance access control solution with the following key features:

### Core Features
- **Hierarchical Rule System**: N-ary tree structure for efficient path-based rule lookup
- **YAML Configuration**: Human-readable ACL files (`syft.pub.yaml`) with clear syntax
- **Owner-Based Access**: Automatic full access for datasite owners
- **Terminal Nodes**: Prevent inheritance for simplified security management

### Advanced Pattern Matching
- **Glob Patterns**: Standard wildcard matching (`*`, `**`, `?`, `[abc]`)
- **Template Patterns**: Dynamic resolution using Go templates
- **USER Token**: Automatic resolution to requesting user's ID
- **Template Functions**: Built-in functions for string manipulation and hashing
- **Domain-Based Access**: Glob patterns in access lists for domain-wide permissions

### Performance Optimizations
- **LRU Cache with TTL**: 100K entry cache with 1-hour expiration
- **Parallel Processing**: 16-worker concurrent ACL file fetching
- **Efficient Tree Traversal**: O(d) complexity where d = path depth
- **Memory Optimization**: Object pooling and lazy loading

### Security Features
- **Template Security**: Sanitized variables and limited function set
- **File Limits**: Size, count, and type restrictions
- **Path Depth Limits**: 255-level maximum to prevent attacks
- **ACL File Protection**: Automatic elevation to admin level

### Access Levels
- **Read** (Level 1): View file contents and metadata
- **Create** (Level 2): Create new files
- **Write** (Level 4): Modify/delete files
- **Admin** (Level 8): Full control including ACL modifications

### Integration Points
- **Blob Service**: Real-time ACL file updates
- **Sync Engine**: Permission validation for file operations
- **WebSocket Events**: Real-time notifications and cache invalidation

The system is designed to be both powerful and user-friendly, supporting complex access control scenarios while maintaining high performance and security standards.
