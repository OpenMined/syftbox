# Conflict Resolution

## Overview

The SyftBox sync system handles conflicts when multiple clients modify the same file or when file-vs-directory conflicts occur. The system uses a "consistent history wins" strategy - whoever has an unbroken chain of edits from the last known state gets their version accepted. In practice, the owner's writes via the server establish the authoritative history, but other users with ACL write permission can also make successful edits if their history is consistent.

## Conflict Types

### 1. Content Conflicts

When the same file is modified on multiple devices simultaneously:

- **Detection**: Both local and remote ETags differ from the journal's last-synced ETag
- **Resolution**: The version with consistent history wins; divergent version preserved as `.conflict` file

```
file.txt (server version - active)
file.conflict.20241212153045 (local version - preserved)
```

### 2. File-vs-Directory Conflicts

When one client creates a file and another creates a directory with the same name:

- **Detection**: Server has file `item`, local has directory `item/` (or vice versa)
- **Resolution**: Timestamp comparison - newer wins

```
# If remote file is newer:
item/ → item.conflict.20241212153045/  (directory moved)
item  (file downloaded)

# If local directory is newer:
Skip download, keep local directory
```

## Resolution Strategies

### Consistent History Wins

For content conflicts, the version with a consistent edit history takes precedence:

1. If a user's local ETag matches the server's previous ETag (consistent history), their upload succeeds
2. If a user's local ETag differs from the server's current ETag (divergent history), conflict is detected
3. Divergent local changes moved to `.conflict` marker file
4. Server's current version downloaded as the active file
5. User can manually review and merge if needed

**Key point**: Any user with ACL write permission can successfully update a file if they're editing from the latest version. Conflicts only occur when two users edit the same version simultaneously.

### Timestamp-Based (Type Conflicts)

For file-vs-directory conflicts:

1. Compare local modification time with remote `LastModified`
2. Newer version wins
3. Older version preserved as `.conflict` marker

## Marker Files

### Conflict Markers

- **Pattern**: `filename.conflict.YYYYMMDDHHMMSS`
- **Purpose**: Preserve local changes for manual review
- **Cleanup**: Delete marker file after resolving

### Rejected Markers

- **Pattern**: `filename.rejected.YYYYMMDDHHMMSS`
- **Purpose**: Indicate permission-denied uploads
- **Cause**: ACL prevents write access

## Cross-Datasite Writes

Clients can write to other users' datasites when granted ACL permission:

1. Client attempts upload to `alice@example.com/shared/file.txt`
2. Server validates ACL permissions
3. If authorized: upload succeeds
4. If denied: file marked as `.rejected`, original re-downloaded

**Note**: Permission validation happens server-side. The client attempts all uploads and handles rejections gracefully.

## Conflict Scenarios

### Scenario 1: Simultaneous Edit

```
Alice: edits file.txt → v2
Bob:   edits file.txt → v3 (at same time)

Result:
- Alice uploaded first, so v2 is on server
- Bob's edit was from v1 (divergent history), so conflict detected
- Bob downloads v2, his v3 → file.conflict.TIMESTAMP
```

### Scenario 2: Offline Divergence

```
Alice: edits file.txt → v2 (online)
Bob:   edits file.txt → v3 (offline)
Bob:   reconnects

Result:
- Conflict detected (journal has v1, local has v3, remote has v2)
- Bob's history diverged, so he gets v2, his v3 → file.conflict.TIMESTAMP
```

### Scenario 3: Directory vs File

```
Alice: creates item/nested.txt
Bob:   creates item (as file)

Resolution depends on timestamps:
- If Bob's file newer: Alice's directory → item.conflict/, Bob's file wins
- If Alice's directory newer: Bob's file upload rejected, Alice's directory preserved
```

## Journal Role

The sync journal (`sync.db`) stores the last successfully synced state:

| Field          | Purpose                         |
| -------------- | ------------------------------- |
| `path`         | File path relative to datasites |
| `etag`         | MD5 hash of last synced content |
| `size`         | File size in bytes              |
| `lastModified` | Timestamp of last sync          |

The journal enables three-way merge by providing the "common ancestor" state.

## Best Practices

1. **Frequent Syncs**: Reduces conflict window
2. **Review Conflicts**: Check `.conflict` files promptly
3. **Coordinate Edits**: For shared files, communicate with collaborators
4. **Use ACLs**: Restrict write access to prevent unauthorized conflicts

## Testing

Run conflict tests:

```bash
just sbdev-test-conflict
```

Tests include:

- `TestSimultaneousWrite` - Two clients edit same file
- `TestDivergentEdits` - Offline client reconnects with changes
- `TestThreeWayConflict` - Three clients edit simultaneously
- `TestNestedPathConflict` - File-vs-directory conflict
- `TestJournalLossRecovery` - Recovery after journal deletion
