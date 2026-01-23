# ACL Sync Notes (Draft)

This document captures design thoughts from the Windows test debugging and
ACL ordering discussions. It is not yet final policy, but a reference for
future implementation.

## Problem: ACL Ordering + Bootstrap

- ACLs (syft.pub.yaml) are the mechanism for access control.
- If ACL files arrive out of order (child before parent), clients can reject
  or delete them because the parent rules are not yet known.
- If ACLs are private and gated by parent ACLs, a client cannot read any ACL
  until it already has access, which creates a bootstrap deadlock.

## Temporary Quick Fix (Not Desired Long Term)

- Treat ACL files as public metadata for read (removed after investigation):
  - Any user can read syft.pub.yaml.
  - ACLs do not grant data access; they only describe it.
  - Real file access is still enforced by rules.
- This avoids the chicken-and-egg issue and prevents clients from dropping
  ACLs during out-of-order delivery.
 - This was added as a quick Windows stabilization fix and is not the
   preferred long-term model.

## Alternative: Server-Computed Visibility Manifest

Goal: keep default deny while avoiding ACL bootstrap/ordering problems.

Idea:
- The server already sees ACL updates first.
- It can compute a per-user "visibility manifest" that lists what that user
  is allowed to sync, without exposing other paths or contents.

Manifest contents (example):
- Allowed prefixes or ACL paths for user (paths or syft pub files).
- ACL version/hash/etag for each entry (so client can fetch/update quickly).

Client flow:
1) Fetch manifest at startup (single operation).
2) Download listed ACLs first (ordered).
3) Bulk sync allowed prefixes in one go.
4) Apply deltas on ACL changes (server pushes updated manifest entries).

Benefits:
- Avoids public ACLs while staying deterministic.
- Avoids out-of-order ACL delivery issues.
- Keeps "default deny" semantics.
- Leaks only the paths a user is explicitly allowed to see.

Trade-offs:
- Requires server-side per-user ACL computation and a new endpoint/WS message.
- Needs client changes to treat manifest as source of truth.

## Optional Hybrid / Transition Plan

- Keep ACLs readable as metadata for now.
- Add manifest endpoint and client support behind a flag.
- Once stable, remove global ACL-read override if desired.
