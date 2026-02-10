#[cfg(unix)]
mod unix_tests {
    // Ensures daemon UX parity: creates log file and exits cleanly on Ctrl+C (SIGINT).
    use std::process::Command;
    use std::time::{Duration, Instant};

    fn wait_for<F: FnMut() -> bool>(timeout: Duration, mut f: F) -> bool {
        let deadline = Instant::now() + timeout;
        while Instant::now() < deadline {
            if f() {
                return true;
            }
            std::thread::sleep(Duration::from_millis(50));
        }
        false
    }

    #[test]
    fn daemon_creates_log_and_exits_on_sigint() {
        let home = std::env::temp_dir().join("syftbox-rs-daemon-ux-home");
        let _ = std::fs::remove_dir_all(&home);
        std::fs::create_dir_all(&home).unwrap();

        let cfg_path = home.join(".syftbox").join("config.json");
        std::fs::create_dir_all(cfg_path.parent().unwrap()).unwrap();
        let data_dir = home.join("SyftBox");
        std::fs::create_dir_all(&data_dir).unwrap();
        std::fs::write(
            &cfg_path,
            format!(
                r#"{{
                  "email":"alice@example.com",
                  "data_dir":"{}",
                  "server_url":"http://127.0.0.1:1"
                }}"#,
                data_dir.display()
            ),
        )
        .unwrap();

        let exe = env!("CARGO_BIN_EXE_syftbox-rs");
        let mut child = Command::new(exe)
            .env("HOME", &home)
            .arg("-c")
            .arg(&cfg_path)
            .arg("daemon")
            .arg("--http-addr")
            .arg("127.0.0.1:0")
            .spawn()
            .expect("spawn daemon");

        let log_path = home.join(".syftbox").join("logs").join("syftbox.log");
        let saw_control_plane = wait_for(Duration::from_secs(3), || {
            std::fs::read_to_string(&log_path)
                .ok()
                .map(|s| s.contains("control plane starting") && s.contains("token="))
                .unwrap_or(false)
        });
        assert!(saw_control_plane, "expected control plane start in log");

        let pid = child.id();
        let status = Command::new("kill")
            .arg("-INT")
            .arg(pid.to_string())
            .status()
            .expect("send SIGINT");
        assert!(status.success());

        let exited = wait_for(Duration::from_secs(3), || {
            child.try_wait().ok().flatten().is_some()
        });
        if !exited {
            let _ = Command::new("kill")
                .arg("-KILL")
                .arg(pid.to_string())
                .status();
            panic!("daemon did not exit after SIGINT");
        }
        let st = child.wait().unwrap();
        assert!(st.success());
    }
}
