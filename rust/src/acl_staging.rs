use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use crate::wsproto::ACLManifest;

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
    on_ready: Option<OnReadyCallback>,
}

impl ACLStagingManager {
    pub fn new<F>(on_ready: F) -> Self
    where
        F: Fn(String, Vec<StagedACL>) + Send + Sync + 'static,
    {
        Self {
            pending: Mutex::new(HashMap::new()),
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
}
