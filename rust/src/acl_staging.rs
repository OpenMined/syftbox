use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use crate::wsproto::ACLManifest;

/// Grace period after ACL set is applied - protects ACL files from deletion
/// during the window when remote state might not yet reflect the new ACLs.
/// Extended to 30 seconds to handle cases where:
/// 1. User A revokes User B's access (takes ~10+ seconds for non-replication check)
/// 2. User A grants User B access again
/// 3. User B's grace window from baseline may have expired during step 1
/// The proper fix is server-side (send empty manifests to users who lose access),
/// but this longer grace period provides client-side resilience.
const ACL_GRACE_PERIOD: Duration = Duration::from_secs(30);

#[derive(Debug, Clone)]
pub struct StagedACL {
    pub path: String,
    pub content: Vec<u8>,
    pub etag: String,
}

struct PendingACLSet {
    manifest: ACLManifest,
    received: HashMap<String, StagedACL>,
    applied: bool,
}

impl PendingACLSet {
    fn is_complete(&self) -> bool {
        for entry in &self.manifest.acl_order {
            if !self.received.contains_key(&entry.path) {
                return false;
            }
        }
        true
    }

    fn received_count(&self) -> usize {
        self.received.len()
    }

    fn expected_count(&self) -> usize {
        self.manifest.acl_order.len()
    }
}

type OnReadyCallback = Arc<dyn Fn(String, Vec<StagedACL>) + Send + Sync>;

pub struct ACLStagingManager {
    pending: Mutex<HashMap<String, PendingACLSet>>,
    /// Tracks when ACL sets were recently applied per datasite.
    /// Used to provide a grace window after application to prevent
    /// spurious deletions before remote state catches up.
    recent: Mutex<HashMap<String, Instant>>,
    on_ready: Option<OnReadyCallback>,
}

impl ACLStagingManager {
    pub fn new<F>(on_ready: F) -> Self
    where
        F: Fn(String, Vec<StagedACL>) + Send + Sync + 'static,
    {
        Self {
            pending: Mutex::new(HashMap::new()),
            recent: Mutex::new(HashMap::new()),
            on_ready: Some(Arc::new(on_ready)),
        }
    }

    pub fn set_manifest(&self, manifest: ACLManifest) {
        let mut pending = self.pending.lock().expect("acl staging lock");
        let datasite = manifest.datasite.clone();

        if let Some(existing) = pending.get(&datasite) {
            if !existing.applied {
                crate::logging::info(format!(
                    "acl staging replacing pending manifest datasite={} oldCount={} newCount={}",
                    datasite,
                    existing.expected_count(),
                    manifest.acl_order.len()
                ));
            }
        }

        pending.insert(
            datasite.clone(),
            PendingACLSet {
                manifest,
                received: HashMap::new(),
                applied: false,
            },
        );

        crate::logging::info(format!(
            "acl staging manifest set datasite={} expectedCount={}",
            datasite,
            pending
                .get(&datasite)
                .map(|p| p.expected_count())
                .unwrap_or(0)
        ));
    }

    pub fn stage_acl(&self, datasite: &str, path: &str, content: Vec<u8>, etag: String) -> bool {
        let mut pending_guard = self.pending.lock().expect("acl staging lock");
        let pending = match pending_guard.get_mut(datasite) {
            Some(p) if !p.applied => p,
            _ => return false,
        };

        let is_expected = pending
            .manifest
            .acl_order
            .iter()
            .any(|entry| entry.path == path);

        if !is_expected {
            crate::logging::info(format!(
                "acl staging unexpected path datasite={} path={}",
                datasite, path
            ));
            return false;
        }

        pending.received.insert(
            path.to_string(),
            StagedACL {
                path: path.to_string(),
                content,
                etag,
            },
        );

        crate::logging::info(format!(
            "acl staging received datasite={} path={} received={} expected={}",
            datasite,
            path,
            pending.received_count(),
            pending.expected_count()
        ));

        if pending.is_complete() {
            crate::logging::info(format!(
                "acl staging complete datasite={} count={}",
                datasite,
                pending.expected_count()
            ));
            pending.applied = true;

            // Record the time when this ACL set was applied for grace window tracking
            {
                let mut recent = self.recent.lock().expect("acl recent lock");
                recent.insert(datasite.to_string(), Instant::now());
                crate::logging::info(format!(
                    "acl staging grace window started datasite={} duration={:?}",
                    datasite, ACL_GRACE_PERIOD
                ));
            }

            let ordered_acls: Vec<StagedACL> = pending
                .manifest
                .acl_order
                .iter()
                .filter_map(|entry| pending.received.get(&entry.path).cloned())
                .collect();

            if let Some(ref on_ready) = self.on_ready {
                let callback = on_ready.clone();
                let ds = datasite.to_string();
                drop(pending_guard);
                callback(ds, ordered_acls);
            }
        }

        true
    }

    pub fn has_pending_manifest(&self, datasite: &str) -> bool {
        let pending = self.pending.lock().expect("acl staging lock");
        pending.get(datasite).is_some_and(|p| !p.applied)
    }

    #[allow(dead_code)]
    pub fn get_pending_paths(&self, datasite: &str) -> Vec<String> {
        let pending = self.pending.lock().expect("acl staging lock");
        match pending.get(datasite) {
            Some(p) => p
                .manifest
                .acl_order
                .iter()
                .map(|e| e.path.clone())
                .collect(),
            None => Vec::new(),
        }
    }

    /// Check if a relative path is a pending ACL file that shouldn't be deleted.
    /// This matches Go's IsPendingACLPath behavior with grace window support.
    pub fn is_pending_acl_path(&self, rel_path: &str) -> bool {
        // Normalize path separators for Windows compatibility and handle leading slashes
        let normalized_path = rel_path.replace('\\', "/");
        let normalized_path = normalized_path.trim_start_matches('/');

        // Extract datasite from the path (first component, e.g., "alice@example.com")
        let datasite = match normalized_path.split('/').next() {
            Some(ds) if !ds.is_empty() => ds,
            _ => {
                crate::logging::info(format!(
                    "acl staging is_pending_acl_path no datasite path={}",
                    rel_path
                ));
                return false;
            }
        };

        // Check 1: Grace window for ACL files (like Go's m.recent check)
        // If this is a syft.pub.yaml file and we recently applied ACLs for this datasite,
        // protect it from deletion during the grace window.
        let is_acl_file = normalized_path.ends_with("/syft.pub.yaml")
            || normalized_path.ends_with("\\syft.pub.yaml");

        if is_acl_file {
            let recent = self.recent.lock().expect("acl recent lock");
            let recent_keys: Vec<_> = recent.keys().collect();
            crate::logging::info(format!(
                "acl staging grace check path={} datasite={} recent_datasites={:?}",
                rel_path, datasite, recent_keys
            ));
            if let Some(applied_at) = recent.get(datasite) {
                let elapsed = applied_at.elapsed();
                if elapsed <= ACL_GRACE_PERIOD {
                    crate::logging::info(format!(
                        "acl staging grace window protecting path={} datasite={} elapsed={:?} remaining={:?}",
                        rel_path, datasite, elapsed, ACL_GRACE_PERIOD - elapsed
                    ));
                    return true;
                } else {
                    crate::logging::info(format!(
                        "acl staging grace window EXPIRED path={} datasite={} elapsed={:?} grace_period={:?}",
                        rel_path, datasite, elapsed, ACL_GRACE_PERIOD
                    ));
                }
            } else {
                crate::logging::info(format!(
                    "acl staging grace window NOT FOUND path={} datasite={} recent_datasites={:?}",
                    rel_path, datasite, recent_keys
                ));
            }
        }

        // Check 2: Pending manifest (ACL set not yet fully applied)
        let pending = self.pending.lock().expect("acl staging lock");
        match pending.get(datasite) {
            Some(p) if !p.applied => {
                // Check if this path matches any expected ACL path
                // Go builds: aclFilePath := normalizedEntry + "/syft.pub.yaml"
                // Then checks: normalizedPath == aclFilePath || normalizedPath == normalizedEntry
                let matches = p.manifest.acl_order.iter().any(|entry| {
                    let normalized_entry = entry.path.replace('\\', "/");
                    let acl_file_path = format!("{}/syft.pub.yaml", normalized_entry);
                    normalized_path == acl_file_path || normalized_path == normalized_entry
                });
                if matches {
                    crate::logging::info(format!(
                        "acl staging pending manifest protecting path={} datasite={}",
                        rel_path, datasite
                    ));
                }
                matches
            }
            _ => false,
        }
    }

    /// Prune expired entries from the recent map.
    /// Called periodically to clean up old grace window entries.
    #[allow(dead_code)]
    pub fn prune_expired(&self) {
        let mut recent = self.recent.lock().expect("acl recent lock");
        recent.retain(|datasite, applied_at| {
            let dominated = applied_at.elapsed() <= ACL_GRACE_PERIOD;
            if !dominated {
                crate::logging::info(format!(
                    "acl staging grace window expired datasite={}",
                    datasite
                ));
            }
            dominated
        });
    }

    /// Note ACL activity for a datasite - refreshes the grace window.
    /// This should be called whenever an ACL file is received (via WebSocket),
    /// regardless of whether there's a pending manifest. This matches Go's
    /// NoteACLActivity behavior which protects ACL files from deletion
    /// even when the user doesn't receive a new manifest (e.g., when access
    /// is revoked but no new manifest is sent to the denied user).
    pub fn note_acl_activity(&self, datasite: &str) {
        if datasite.is_empty() {
            return;
        }
        let mut recent = self.recent.lock().expect("acl recent lock");
        recent.insert(datasite.to_string(), Instant::now());
        crate::logging::info(format!(
            "acl staging activity noted datasite={} grace_period={:?}",
            datasite, ACL_GRACE_PERIOD
        ));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::wsproto::{ACLEntry, ACLManifest};
    use std::sync::atomic::{AtomicBool, Ordering};

    #[test]
    fn test_staging_complete_triggers_callback() {
        let called = Arc::new(AtomicBool::new(false));
        let called_clone = called.clone();

        let manager = ACLStagingManager::new(move |_datasite, _acls| {
            called_clone.store(true, Ordering::SeqCst);
        });

        let manifest = ACLManifest {
            version: 1,
            datasite: "test@example.com".to_string(),
            for_user: "other@example.com".to_string(),
            for_hash: "abc123".to_string(),
            generated: "2024-01-01T00:00:00Z".to_string(),
            acl_order: vec![
                ACLEntry {
                    path: "test@example.com".to_string(),
                    hash: "h1".to_string(),
                },
                ACLEntry {
                    path: "test@example.com/public".to_string(),
                    hash: "h2".to_string(),
                },
            ],
        };

        manager.set_manifest(manifest);
        assert!(manager.has_pending_manifest("test@example.com"));
        assert!(!called.load(Ordering::SeqCst));

        manager.stage_acl(
            "test@example.com",
            "test@example.com",
            b"acl1".to_vec(),
            "etag1".to_string(),
        );
        assert!(!called.load(Ordering::SeqCst));

        manager.stage_acl(
            "test@example.com",
            "test@example.com/public",
            b"acl2".to_vec(),
            "etag2".to_string(),
        );
        assert!(called.load(Ordering::SeqCst));
        assert!(!manager.has_pending_manifest("test@example.com"));
    }

    #[test]
    fn test_unexpected_path_rejected() {
        let manager = ACLStagingManager::new(|_, _| {});

        let manifest = ACLManifest {
            version: 1,
            datasite: "test@example.com".to_string(),
            for_user: "other@example.com".to_string(),
            for_hash: "abc123".to_string(),
            generated: "2024-01-01T00:00:00Z".to_string(),
            acl_order: vec![ACLEntry {
                path: "test@example.com".to_string(),
                hash: "h1".to_string(),
            }],
        };

        manager.set_manifest(manifest);

        let staged = manager.stage_acl(
            "test@example.com",
            "test@example.com/unexpected",
            b"acl".to_vec(),
            "etag".to_string(),
        );
        assert!(!staged);
    }

    #[test]
    fn test_grace_window_protects_acl_files() {
        let manager = ACLStagingManager::new(|_, _| {});

        let manifest = ACLManifest {
            version: 1,
            datasite: "bob@example.com".to_string(),
            for_user: "charlie@example.com".to_string(),
            for_hash: "abc123".to_string(),
            generated: "2024-01-01T00:00:00Z".to_string(),
            acl_order: vec![
                ACLEntry {
                    path: "bob@example.com".to_string(),
                    hash: "h1".to_string(),
                },
                ACLEntry {
                    path: "bob@example.com/public".to_string(),
                    hash: "h2".to_string(),
                },
            ],
        };

        // Before manifest, path is not protected
        assert!(!manager.is_pending_acl_path("bob@example.com/public/syft.pub.yaml"));

        // Set manifest - now pending protection kicks in
        manager.set_manifest(manifest);
        assert!(manager.is_pending_acl_path("bob@example.com/public/syft.pub.yaml"));

        // Stage first ACL
        manager.stage_acl(
            "bob@example.com",
            "bob@example.com",
            b"acl1".to_vec(),
            "etag1".to_string(),
        );
        // Still protected (pending)
        assert!(manager.is_pending_acl_path("bob@example.com/public/syft.pub.yaml"));

        // Stage second ACL - completes the set, triggers grace window
        manager.stage_acl(
            "bob@example.com",
            "bob@example.com/public",
            b"acl2".to_vec(),
            "etag2".to_string(),
        );

        // Manifest is now applied, but grace window should still protect
        assert!(!manager.has_pending_manifest("bob@example.com"));
        // The grace window should protect the ACL file for ACL_GRACE_PERIOD
        assert!(manager.is_pending_acl_path("bob@example.com/public/syft.pub.yaml"));
    }

    #[test]
    fn test_pending_manifest_path_matching() {
        let manager = ACLStagingManager::new(|_, _| {});

        let manifest = ACLManifest {
            version: 1,
            datasite: "alice@example.com".to_string(),
            for_user: "bob@example.com".to_string(),
            for_hash: "abc123".to_string(),
            generated: "2024-01-01T00:00:00Z".to_string(),
            acl_order: vec![
                ACLEntry {
                    path: "alice@example.com".to_string(),
                    hash: "h1".to_string(),
                },
                ACLEntry {
                    path: "alice@example.com/public".to_string(),
                    hash: "h2".to_string(),
                },
            ],
        };

        manager.set_manifest(manifest);

        // Should match the ACL file path (entry + /syft.pub.yaml)
        assert!(manager.is_pending_acl_path("alice@example.com/syft.pub.yaml"));
        assert!(manager.is_pending_acl_path("alice@example.com/public/syft.pub.yaml"));

        // Should match the entry path itself
        assert!(manager.is_pending_acl_path("alice@example.com"));
        assert!(manager.is_pending_acl_path("alice@example.com/public"));

        // Should NOT match unrelated paths
        assert!(!manager.is_pending_acl_path("alice@example.com/private/syft.pub.yaml"));
        assert!(!manager.is_pending_acl_path("bob@example.com/public/syft.pub.yaml"));
    }
}
