# syft.sub.yaml plan

## Goals

- Provide client-side subscriptions so users explicitly choose which remote datasite paths to receive.
- Default to receiving no remote files until subscribed (opt-in only).
- Keep discovery intact: always sync ACL metadata so users can see what is available.
- Unsubscribing must never publish deletes or other side effects.

## Non-goals (for now)

- Per-device subscriptions and device scoping.
- Server-side enforcement changes (subscription is client-only).

## File and placement

- Canonical name: `syft.sub.yaml`.
- Path: `.data/syft.sub.yaml` (local-only, never synced).
- Ownership: local client state; not published or shared.
- Optional future: support nested `syft.sub.yaml` with `terminal: true|false` like ACLs.
- Treat `syft.sub.yaml` as a special file that is never synced or uploaded, even if placed under `datasites/`.

## Schema (v1)

```yaml
version: 1
defaults:
  action: deny
rules:
  - action: allow
    datasite: "alice@example.com"
    path: "public/**"
  - action: allow
    datasite: "bob@example.com"
    path: "projects/team/**"
  - action: block
    datasite: "bob@example.com"
    path: "projects/team/tmp/**"

### Syntax alignment with `syft.pub.yaml`

- Uses `rules:` list with `path` glob patterns, similar to ACL `rules:` with `pattern`.
- Uses `datasite` to scope rules by owner; when omitted, `path` can include the datasite prefix.
- Avoids ACL-specific keys like `access` and `limits` to keep semantics distinct.

### Example comparison

`syft.pub.yaml` (publish/ACL):

```yaml
terminal: false
rules:
  - pattern: "public/**"
    access:
      read: ["*"]
      write: []
```

`syft.sub.yaml` (subscribe):

```yaml
version: 1
defaults:
  remote: deny
rules:
  - action: allow
    datasite: "alice@example.com"
    path: "public/**"
  - action: pause
    datasite: "bob@example.com"
    path: "projects/team/**"
  - action: block
    datasite: "bob@example.com"
    path: "projects/team/tmp/**"
```
```

### Fields

- `defaults.action`: `allow`, `pause`, or `block`. This only applies to remote datasites. Local datasite is always allowed.
- `deny` is accepted as an alias for `block` for compatibility.
- `rules[]`:
  - `action`: `allow`, `pause`, or `block`.
  - `datasite` (optional): exact email or glob (e.g., `*@example.com`). If omitted, `path` must include the datasite prefix.
  - `path`: glob under the datasite root (examples: `public/**`, `projects/**`). If `datasite` is omitted, use `path` like `alice@example.com/public/**`.
  - `pause` stops syncing and keeps local data.
  - `block` stops syncing and prunes local data (local-only delete, no remote side effects).

### Resolution

- Normalize key to `datasite/path`.
- If `datasite` is omitted, parse it from the first path segment.
- Evaluate rules in order; last match wins.
- If no rules match, fall back to `defaults.action`.
- Rules never apply to the local datasite.

## Behavior

### Default

- Remote files are not downloaded unless explicitly allowed by `syft.sub.yaml` (opt-in only).
- ACL metadata is always synced for discovery: `**/syft.pub.yaml` and ACL manifests.

### Subscribe

- When a rule allows a remote datasite/path, normal sync applies subject to ACL checks.
- ACL remains the server-side authority; subscription is client-side filtering only.

### Unsubscribe

- Stop downloading and stop uploading for that datasite/path.
- `pause` keeps local copies and drops any sync queue entries.
- `block` deletes local copies and drops journal entries (no remote deletes).
- Never publish deletes or write operations for unsubscribed paths.

## Edge cases

- Invalid `syft.sub.yaml`: keep last valid config or fall back to deny-all; log a warning.
- WS pushes: ignore non-ACL file writes when unsubscribed; still accept ACL metadata.
- ACL allows, sub denies: local stays unsubscribed (no sync). Surface as "subscribed=no" UI state.
- Sub allows, ACL denies: no data arrives; surface as "no access" UI state.
- Syft.pub.yaml updates: always sync and apply, regardless of subscriptions.
- Datasite rename/vanity domains: match on canonical email-based datasite key.

## Implementation hooks

### Go client

- Filter remote state in `internal/client/sync/sync_engine.go` before reconcile.
- Filter WS priority downloads in `internal/client/sync/sync_engine_priority_download.go`.
- Ensure local-only prune does not enqueue remote deletes or uploads.
- Parse and cache `syft.sub.yaml` (new helper in `internal/client/sync/`).

### Rust client

- Filter remote scan in `rust/src/sync.rs`.
- Filter WS file writes in `rust/src/client.rs`.
- Mirror Go behavior for prune and ACL metadata exceptions.

## Discovery

- Keep syncing ACL metadata so users can see what exists without subscribing.
- Optional future: add a server endpoint to list ACL manifests or datasites.

## Control plane APIs

Add control plane endpoints so apps can query and modify subscription state and observe sync activity.

### Read APIs

- `GET /v1/subscriptions` returns parsed `syft.sub.yaml`, defaults, and effective rules.
- `GET /v1/subscriptions/effective` returns a per-datasite/path decision view for UI use.
- `GET /v1/publications` returns derived publish info from `syft.pub.yaml` rules (owner scope).
- `GET /v1/sync/queue` returns current pending operations (download, upload, delete) with counts.
- `GET /v1/sync/status` returns per-path sync state (syncing, pending, rejected, conflicted).
- `GET /v1/discovery/files` returns metadata (path, size, mtime, etag) for accessible files not currently synced due to pause/block.

### Write APIs

- `PUT /v1/subscriptions` updates `syft.sub.yaml` atomically with validation.
- `POST /v1/subscriptions/rules` adds or updates a rule (helper API for UI).
- `DELETE /v1/subscriptions/rules` removes a rule by ID or match.
- `POST /v1/sync/refresh` triggers a full sync.
- `POST /v1/sync/refresh?path=...` triggers a targeted resync for a path.

### Behavior

- Updates to subscriptions should immediately re-evaluate queue and apply prune if enabled.
- API should surface conflicts between subscription and ACL (e.g., allowed but no access).
- Control plane writes update `.data/syft.sub.yaml` so the client state is durable and inspectable.
- Discovery metadata is read-only and must not trigger downloads or content access.

## Progress

- Added a short ACL propagation wait in `cmd/devstack/conflict_test.go` to stabilize `TestRapidSequentialEdits`.
- Fixed Rust control plane `subscriptions_put` response type in `rust/src/control.rs` and removed an unused import.
- Ran `just sbdev-test-all` in Go mode (pass).
- Ran `just sbdev-test-all mode=rust` in Rust mode (pass).
