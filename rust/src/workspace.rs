use std::fs;
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};

#[derive(Debug)]
pub struct WorkspaceLockedError;

impl std::fmt::Display for WorkspaceLockedError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "workspace locked by another process")
    }
}

impl std::error::Error for WorkspaceLockedError {}

#[derive(Debug)]
pub struct WorkspaceLock {
    #[allow(dead_code)]
    file: fs::File,
    path: PathBuf,
}

pub fn ensure_workspace_layout(data_dir: &Path, email: &str) -> Result<()> {
    let apps_dir = data_dir.join("apps");
    let meta_dir = data_dir.join(".data");
    let datasites_dir = data_dir.join("datasites");
    let root_dir = datasites_dir.join(email);
    let public_dir = root_dir.join("public");

    fs::create_dir_all(&apps_dir).with_context(|| format!("create {}", apps_dir.display()))?;
    fs::create_dir_all(&meta_dir).with_context(|| format!("create {}", meta_dir.display()))?;
    fs::create_dir_all(&public_dir).with_context(|| format!("create {}", public_dir.display()))?;

    let root_acl = root_dir.join("syft.pub.yaml");
    if !root_acl.exists() {
        let content = "terminal: false\nrules:\n  - pattern: '**'\n    access:\n      admin: []\n      write: []\n      read: []\n";
        fs::write(&root_acl, content).with_context(|| format!("write {}", root_acl.display()))?;
    }

    let public_acl = public_dir.join("syft.pub.yaml");
    if !public_acl.exists() {
        let content =
            "terminal: false\nrules:\n  - pattern: '**'\n    access:\n      admin: []\n      write: []\n      read: ['*']\n";
        fs::write(&public_acl, content)
            .with_context(|| format!("write {}", public_acl.display()))?;
    }

    Ok(())
}

impl WorkspaceLock {
    pub fn try_lock(data_dir: &Path) -> Result<Self> {
        let meta_dir = data_dir.join(".data");
        fs::create_dir_all(&meta_dir).with_context(|| format!("create {}", meta_dir.display()))?;
        let lock_path = meta_dir.join("syftbox.lock");
        let file = open_lock_file(&lock_path)?;
        lock_file(&file).context("lock")?;

        Ok(Self {
            file,
            path: lock_path,
        })
    }
}

impl Drop for WorkspaceLock {
    fn drop(&mut self) {
        // Best-effort unlock + remove, mirroring Go's Unlock() removing the lock file.
        let _ = unlock_file(&self.file);
        let _ = fs::remove_file(&self.path);
    }
}

#[cfg(unix)]
fn lock_file(file: &fs::File) -> Result<()> {
    use std::os::fd::AsRawFd;
    extern "C" {
        fn flock(fd: i32, operation: i32) -> i32;
    }
    const LOCK_EX: i32 = 2;
    const LOCK_NB: i32 = 4;

    let rc = unsafe { flock(file.as_raw_fd(), LOCK_EX | LOCK_NB) };
    if rc == 0 {
        return Ok(());
    }
    let err = std::io::Error::last_os_error();
    let raw = err.raw_os_error();
    // macOS uses EWOULDBLOCK=35; Linux typically uses EWOULDBLOCK/EAGAIN=11.
    if err.kind() == std::io::ErrorKind::WouldBlock || raw == Some(11) || raw == Some(35) {
        return Err(WorkspaceLockedError.into());
    }
    Err(err).context("flock")
}

#[cfg(unix)]
fn unlock_file(file: &fs::File) -> Result<()> {
    use std::os::fd::AsRawFd;
    extern "C" {
        fn flock(fd: i32, operation: i32) -> i32;
    }
    const LOCK_UN: i32 = 8;
    let rc = unsafe { flock(file.as_raw_fd(), LOCK_UN) };
    if rc == 0 {
        Ok(())
    } else {
        Err(std::io::Error::last_os_error()).context("flock unlock")
    }
}

#[cfg(windows)]
fn lock_file(_file: &fs::File) -> Result<()> {
    // open_lock_file() uses create_new so locking is implicit.
    Ok(())
}

#[cfg(windows)]
fn unlock_file(_file: &fs::File) -> Result<()> {
    Ok(())
}

#[cfg(unix)]
fn open_lock_file(lock_path: &Path) -> Result<fs::File> {
    fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(false)
        .open(lock_path)
        .with_context(|| format!("open {}", lock_path.display()))
}

#[cfg(windows)]
fn open_lock_file(lock_path: &Path) -> Result<fs::File> {
    // Emulate an exclusive lock by atomically creating the file.
    let file = fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create_new(true)
        .open(lock_path);
    match file {
        Ok(f) => Ok(f),
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => Err(WorkspaceLockedError.into()),
        Err(e) => Err(e).with_context(|| format!("open {}", lock_path.display())),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ensure_workspace_layout_creates_dirs_and_acls() {
        let tmp = std::env::temp_dir().join("syftbox-rs-workspace-test");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();

        ensure_workspace_layout(&tmp, "alice@example.com").unwrap();
        assert!(tmp.join("apps").is_dir());
        assert!(tmp.join(".data").is_dir());
        assert!(tmp
            .join("datasites")
            .join("alice@example.com")
            .join("public")
            .is_dir());
        assert!(tmp
            .join("datasites")
            .join("alice@example.com")
            .join("syft.pub.yaml")
            .is_file());
        assert!(tmp
            .join("datasites")
            .join("alice@example.com")
            .join("public")
            .join("syft.pub.yaml")
            .is_file());
    }

    #[test]
    fn workspace_lock_is_exclusive_and_released_on_drop() {
        let tmp = std::env::temp_dir().join("syftbox-rs-workspace-lock-test");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();

        let lock1 = WorkspaceLock::try_lock(&tmp).unwrap();
        let err = WorkspaceLock::try_lock(&tmp).unwrap_err();
        let mut found = false;
        for cause in err.chain() {
            if cause.is::<WorkspaceLockedError>() {
                found = true;
                break;
            }
        }
        assert!(found, "expected WorkspaceLockedError, got: {err:#}");

        drop(lock1);
        let _lock2 = WorkspaceLock::try_lock(&tmp).unwrap();
    }
}
