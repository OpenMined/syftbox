# Sync System

## Overview

The SyftBox sync system implements a bidirectional file synchronization mechanism between local datasites and remote servers. It uses a three-way merge algorithm to reconcile changes and handles conflicts, permissions, and error conditions gracefully.

## Architecture

### Core Components

- **SyncManager**: High-level orchestrator that manages the sync engine lifecycle
- **SyncEngine**: Main sync orchestrator that coordinates all sync operations
- **SyncJournal**: SQLite-based persistent storage of last known sync state
- **SyncLocalState**: Local filesystem scanner and state tracker
- **SyncStatus**: In-memory status tracking with event broadcasting
- **FileWatcher**: Real-time file change detection with debouncing
- **SyncIgnoreList**: Gitignore-style file filtering system
- **SyncPriorityList**: Priority-based sync handling for specific file types

### File Organization

The sync system operates on the `datasites/` directory structure and maintains metadata in a separate `.syftbox/sync.db` SQLite database.

## Sync Algorithm

### 1. Presync Checks
- Verify workspace is writable
- Ensure minimum free disk space (5GB)
- Check network connectivity

### 2. State Collection
The sync engine collects three states for comparison:

- **Remote State**: File metadata from the server via SyftSDK
- **Local State**: Current filesystem state from directory scan
- **Journal State**: Last known sync state from SQLite database

### 3. Three-Way Reconciliation

The reconcile algorithm performs a three-way diff between local, remote, and journal states to determine required operations:

```
Operation Types:
- OpWriteRemote: Upload local changes to server
- OpWriteLocal: Download remote changes locally  
- OpDeleteRemote: Delete file from server
- OpDeleteLocal: Delete local file
- OpConflict: Handle conflicting changes
```

#### Decision Logic

For each file path across all three states:

**Conflicts** (require user intervention):
- Local modified + Remote modified
- Local created + Remote created

**Regular Sync Operations**:
- Local created/modified + Remote unchanged → Upload (`OpWriteRemote`)
- Local unchanged + Remote created/modified → Download (`OpWriteLocal`)
- Local deleted + Remote exists → Delete from server (`OpDeleteRemote`)
- Remote deleted + Local exists → Delete locally (`OpDeleteLocal`)
- Both deleted cleanly → Cleanup journal entry

**Ignored**:
- Files currently syncing
- Files matching ignore patterns
- Empty files (0 bytes)

### 4. Parallel Execution

Operations are executed in parallel batches:
- Remote writes (uploads)
- Local writes (downloads)
- Remote deletes
- Local deletes
- Conflict handling
- Journal cleanup

## File Filtering

### Ignore System

The sync system uses a `.gitignore`-style filtering mechanism:

**Default Ignored Patterns**:
```
syftignore
**/*.conflict.*
**/*.rejected.*
.syftkeep
.ipynb_checkpoints/
__pycache__/
*.py[cod]
.vscode
.idea
.git
*.tmp
*.log
.DS_Store
```

**Custom Rules**: Users can create a `syftignore` file in the datasites directory to add custom ignore patterns.

### Priority System

Priority files are synced immediately upon detection:
- `**/*.request` - API request files
- `**/*.response` - API response files

Priority files trigger immediate sync cycles via the file watcher.

## Real-Time Sync

### File Watcher

The file watcher monitors the datasites directory for changes using OS-level filesystem notifications:

- **Events**: File write operations
- **Debouncing**: 50ms timeout to batch rapid changes
- **Filtering**: Excludes ignored files, non-priority files, and marker files
- **Triggering**: Initiates sync cycles for priority files

### Sync Cycles

- **Full Sync**: Complete reconciliation every 5 seconds
- **Priority Sync**: Immediate sync for priority file changes
- **Initial Sync**: Full sync on startup before starting file watcher

## Error Handling

### Transient Errors (Auto-Recoverable)

These errors are automatically retried in subsequent sync cycles:

**Network Errors**:
- DNS resolution failures
- TLS handshake errors
- Connection timeouts
- Transport errors

**Disk Errors**:
- Permission denied (temporary)
- File locked by another process
- Insufficient disk space
- Cross-device link errors

### Human-Resolvable Errors

These errors require user intervention and create marker files:

#### Permission Rejections

When upload is rejected due to insufficient permissions:

1. **File Creation Rejection**:
   ```
   original_file.txt → original_file.rejected.TIMESTAMP
   Status: rejected
   ```

2. **File Edit Rejection**:
   ```
   edited_file.txt → edited_file.rejected.TIMESTAMP
   Re-download original from server
   Status: rejected
   ```

#### Conflicts

When the same file is modified on multiple devices:

1. **Conflict Detection**:
   ```
   Local ETag ≠ Remote ETag (both changed since last sync)
   ```

2. **Conflict Resolution**:
   ```
   conflicted_file.txt → conflicted_file.conflict.TIMESTAMP
   Download server version as conflicted_file.txt
   Status: conflicted
   ```

### Status States

Each file has two status dimensions:

**Sync State**:
- `pending`: Queued for sync
- `syncing`: Currently being processed
- `completed`: Successfully synchronized
- `error`: Failed with transient error

**Conflict State**:
- `none`: Normal file
- `conflicted`: Has conflict marker file
- `rejected`: Has rejection marker file

## File Markers

The system creates timestamped marker files for human resolution:

### Marker Types
- `.conflict`: Indicates conflicting changes
- `.rejected`: Indicates permission rejections

### Marker Format
```
original_filename.{marker_type}.YYYYMMDDHHMMSS
```

### Resolution Process
Users resolve conflicts/rejections by:
1. Reviewing the original file and marker file
2. Making necessary changes
3. Deleting the marker file
4. File automatically returns to normal sync state

## Database Schema

The sync journal uses SQLite with the following schema:

```sql
CREATE TABLE sync_journal (
    path TEXT PRIMARY KEY,
    etag TEXT NOT NULL,
    version TEXT NOT NULL,
    size INTEGER NOT NULL,
    last_modified TEXT NOT NULL
);
```

This tracks the last known state of each synced file for three-way comparison.

## Configuration

### Sync Constants
- Minimum free space: 5GB
- Full sync interval: 5 seconds
- File watcher debounce: 50ms
- Max upload concurrency: 8 operations
- Download batch size: 100 files

### File Size Limits
- Empty files (0 bytes) are ignored
- No explicit upper size limit (limited by available disk space)

## Event System

The sync status system provides event broadcasting for real-time status updates:

```go
type SyncStatusEvent struct {
    Path   SyncPath
    Status *PathStatus
}
```

Applications can subscribe to these events for UI updates or monitoring.
