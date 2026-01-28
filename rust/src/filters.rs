use std::{
    fs,
    io::{BufRead, BufReader},
    path::{Path, PathBuf},
};

use anyhow::{Context, Result};
use ignore::gitignore::{Gitignore, GitignoreBuilder};

const DEFAULT_IGNORE_LINES: &[&str] = &[
    // syft
    "syftignore",
    "**/syft.sub.yaml",
    "**/*syftrejected*", // legacy marker
    "**/*syftconflict*", // legacy marker
    "**/*.conflict.*",
    "**/*.conflict",
    "**/*.rejected.*",
    "**/*.rejected",
    "**/stream.sock",
    "**/stream.pipe",
    "**/stream.tcp",
    "**/stream.accept",
    "*.syft.tmp.*", // temporary files (Go atomic writes)
    "**/.*.tmp-*",  // temporary files (Rust download temp)
    "**/*.tmp-*",   // temporary files (without leading dot)
    ".syftkeep",
    // python
    ".ipynb_checkpoints/",
    "__pycache__/",
    "*.py[cod]",
    "dist/",
    "venv/",
    ".venv/",
    // IDE/Editor-specific
    ".vscode",
    ".idea",
    // General excludes
    ".git",
    ".data/",
    "*.tmp",
    "*.log",
    "logs/",
    // OS-specific
    ".DS_Store",
    "Thumbds.db",
    "Icon",
];

const DEFAULT_PRIORITY_LINES: &[&str] = &[
    "**/*.request",
    "**/*.response",
    "**/syft.pub.yaml", // ACL files need priority to avoid race condition
];

#[derive(Clone)]
pub struct SyncIgnoreList {
    #[allow(dead_code)]
    base_dir: PathBuf,
    ignore: Gitignore,
}

impl SyncIgnoreList {
    pub fn load(base_dir: &Path) -> Result<Self> {
        let mut builder = GitignoreBuilder::new(base_dir);
        for line in DEFAULT_IGNORE_LINES {
            builder
                .add_line(None, line)
                .with_context(|| format!("add default ignore line: {line}"))?;
        }

        let ignore_path = base_dir.join("syftignore");
        if ignore_path.exists() {
            let custom = read_ignore_file(&ignore_path)?;
            for line in custom {
                builder
                    .add_line(None, &line)
                    .with_context(|| format!("add syftignore line: {line}"))?;
            }
        }

        let ignore = builder.build().context("build ignore matcher")?;
        Ok(Self {
            base_dir: base_dir.to_path_buf(),
            ignore,
        })
    }

    #[allow(dead_code)]
    pub fn should_ignore_abs(&self, abs_path: &Path, is_dir: bool) -> bool {
        let rel = abs_path.strip_prefix(&self.base_dir).unwrap_or(abs_path);
        self.should_ignore_rel(rel, is_dir)
    }

    pub fn should_ignore_rel(&self, rel_path: &Path, is_dir: bool) -> bool {
        self.ignore
            .matched_path_or_any_parents(rel_path, is_dir)
            .is_ignore()
    }
}

#[derive(Clone)]
pub struct SyncPriorityList {
    #[allow(dead_code)]
    base_dir: PathBuf,
    priority: Gitignore,
}

impl SyncPriorityList {
    pub fn load(base_dir: &Path) -> Result<Self> {
        let mut builder = GitignoreBuilder::new(base_dir);
        for line in DEFAULT_PRIORITY_LINES {
            builder
                .add_line(None, line)
                .with_context(|| format!("add default priority line: {line}"))?;
        }
        let priority = builder.build().context("build priority matcher")?;
        Ok(Self {
            base_dir: base_dir.to_path_buf(),
            priority,
        })
    }

    #[allow(dead_code)]
    pub fn should_prioritize_abs(&self, abs_path: &Path, is_dir: bool) -> bool {
        let rel = abs_path.strip_prefix(&self.base_dir).unwrap_or(abs_path);
        self.should_prioritize_rel(rel, is_dir)
    }

    pub fn should_prioritize_rel(&self, rel_path: &Path, is_dir: bool) -> bool {
        self.priority
            .matched_path_or_any_parents(rel_path, is_dir)
            .is_ignore()
    }
}

#[derive(Clone)]
pub struct SyncFilters {
    pub ignore: SyncIgnoreList,
    pub priority: SyncPriorityList,
}

impl SyncFilters {
    pub fn load(datasites_root: &Path) -> Result<Self> {
        fs::create_dir_all(datasites_root)
            .with_context(|| format!("create datasites dir {}", datasites_root.display()))?;
        Ok(Self {
            ignore: SyncIgnoreList::load(datasites_root)?,
            priority: SyncPriorityList::load(datasites_root)?,
        })
    }

    pub fn is_marked_rel_path(rel: &str) -> bool {
        // Equivalent to Go IsMarkedPath checks on filenames (and avoids infinite loops).
        rel.contains(".conflict")
            || rel.contains(".rejected")
            || rel.contains("syftrejected")
            || rel.contains("syftconflict")
    }
}

fn read_ignore_file(path: &Path) -> Result<Vec<String>> {
    let file =
        fs::File::open(path).with_context(|| format!("open ignore file {}", path.display()))?;
    let mut out = Vec::new();
    for line in BufReader::new(file).lines() {
        let line = line?;
        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with('#') || trimmed.contains('\0') {
            continue;
        }
        out.push(trimmed.to_string());
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{fs, time::SystemTime};

    fn make_temp_dir(prefix: &str) -> PathBuf {
        let mut root = std::env::temp_dir();
        let nanos = SystemTime::now()
            .duration_since(SystemTime::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        root.push(format!("{prefix}-{nanos}"));
        fs::create_dir_all(&root).unwrap();
        root
    }

    #[test]
    fn default_ignore_does_not_ignore_request() {
        let root = make_temp_dir("syftbox-rs-ignore-test");
        let ignore = SyncIgnoreList::load(&root).unwrap();
        assert!(!ignore.should_ignore_rel(Path::new("alice/app_data/rpc/x.request"), false));
    }

    #[test]
    fn default_priority_matches_request_response_and_acl() {
        let root = make_temp_dir("syftbox-rs-priority-test");
        let prio = SyncPriorityList::load(&root).unwrap();
        assert!(prio.should_prioritize_rel(Path::new("alice/app_data/rpc/x.request"), false));
        assert!(prio.should_prioritize_rel(Path::new("alice/app_data/rpc/x.response"), false));
        assert!(prio.should_prioritize_rel(Path::new("alice/public/syft.pub.yaml"), false));
    }

    #[test]
    fn default_ignore_matches_rust_download_temp_files() {
        let root = make_temp_dir("syftbox-rs-temp-ignore-test");
        let ignore = SyncIgnoreList::load(&root).unwrap();

        // Rust-style download temp files: .filename.tmp-uuid
        assert!(
            ignore.should_ignore_rel(
                Path::new("alice/public/.syft.pub.yaml.tmp-8cd89f7b-1234"),
                false
            ),
            "Rust download temp file should be ignored"
        );

        assert!(
            ignore.should_ignore_rel(
                Path::new("bob/app_data/.config.json.tmp-abcdef12-3456"),
                false
            ),
            "Rust download temp with dot prefix should be ignored"
        );

        // Temp files without leading dot
        assert!(
            ignore.should_ignore_rel(Path::new("alice/public/data.tmp-12345678"), false),
            "temp file without leading dot should be ignored"
        );
    }

    #[test]
    fn default_ignore_matches_go_atomic_temp_files() {
        let root = make_temp_dir("syftbox-rs-go-temp-ignore-test");
        let ignore = SyncIgnoreList::load(&root).unwrap();

        // Go-style atomic write temp files: *.syft.tmp.*
        assert!(
            ignore.should_ignore_rel(Path::new("alice/public/data.syft.tmp.123456"), false),
            "Go atomic write temp file should be ignored"
        );
    }

    #[test]
    fn regular_files_not_ignored() {
        let root = make_temp_dir("syftbox-rs-regular-test");
        let ignore = SyncIgnoreList::load(&root).unwrap();

        // Regular files should NOT be ignored
        assert!(
            !ignore.should_ignore_rel(Path::new("alice/public/data.txt"), false),
            "regular files should not be ignored"
        );

        // ACL files should NOT be ignored
        assert!(
            !ignore.should_ignore_rel(Path::new("alice/public/syft.pub.yaml"), false),
            "ACL files should not be ignored"
        );
    }
}
