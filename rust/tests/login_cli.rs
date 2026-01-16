use std::process::Command;

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;

fn fake_jwt(payload: &serde_json::Value) -> String {
    let header = serde_json::json!({"alg":"none","typ":"JWT"});
    format!(
        "{}.{}.sig",
        URL_SAFE_NO_PAD.encode(serde_json::to_vec(&header).unwrap()),
        URL_SAFE_NO_PAD.encode(serde_json::to_vec(payload).unwrap())
    )
}

#[test]
fn login_already_logged_in_prints_config() {
    let tmp = std::env::temp_dir().join("syftbox-rs-login-cli-already");
    let _ = std::fs::remove_dir_all(&tmp);
    std::fs::create_dir_all(&tmp).unwrap();

    let cfg_path = tmp.join("config.json");
    let data_dir = tmp.join("SyftBox");
    std::fs::create_dir_all(&data_dir).unwrap();

    let email = "alice@example.com";
    let server_url = "https://syftbox.net";
    let refresh = fake_jwt(&serde_json::json!({
        "type": "refresh",
        "sub": email,
        "exp": 9999999999_i64
    }));
    // Use forward slashes for cross-platform JSON compatibility
    let data_dir_str = data_dir.display().to_string().replace('\\', "/");
    std::fs::write(
        &cfg_path,
        format!(
            r#"{{
              "email":"{email}",
              "data_dir":"{data_dir_str}",
              "server_url":"{server_url}",
              "client_url":"http://127.0.0.1:7938",
              "refresh_token":"{refresh}"
            }}"#,
        ),
    )
    .unwrap();

    let exe = env!("CARGO_BIN_EXE_syftbox-rs");
    let out = Command::new(exe)
        .arg("-c")
        .arg(&cfg_path)
        .arg("login")
        .output()
        .expect("run login");
    assert!(out.status.success());

    let stdout = String::from_utf8_lossy(&out.stdout);
    assert!(stdout.contains("Already logged in"));
    assert!(stdout.contains("SYFTBOX DATASITE CONFIG"));
    assert!(stdout.contains(email));
    // CLI may output path with forward slashes (from config) or native format.
    // Check for either format on Windows.
    let data_dir_native = data_dir.display().to_string();
    let data_dir_forward = data_dir_native.replace('\\', "/");
    assert!(
        stdout.contains(&data_dir_native) || stdout.contains(&data_dir_forward),
        "stdout should contain data_dir path: stdout={stdout}, native={data_dir_native}, forward={data_dir_forward}"
    );
}

#[test]
fn login_already_logged_in_quiet_has_no_output() {
    let tmp = std::env::temp_dir().join("syftbox-rs-login-cli-already-quiet");
    let _ = std::fs::remove_dir_all(&tmp);
    std::fs::create_dir_all(&tmp).unwrap();

    let cfg_path = tmp.join("config.json");
    let data_dir = tmp.join("SyftBox");
    std::fs::create_dir_all(&data_dir).unwrap();

    let email = "alice@example.com";
    let server_url = "https://syftbox.net";
    let refresh = fake_jwt(&serde_json::json!({
        "type": "refresh",
        "sub": email,
        "exp": 9999999999_i64
    }));
    // Use forward slashes for cross-platform JSON compatibility
    let data_dir_str = data_dir.display().to_string().replace('\\', "/");
    std::fs::write(
        &cfg_path,
        format!(
            r#"{{
              "email":"{email}",
              "data_dir":"{data_dir_str}",
              "server_url":"{server_url}",
              "client_url":"http://127.0.0.1:7938",
              "refresh_token":"{refresh}"
            }}"#,
        ),
    )
    .unwrap();

    let exe = env!("CARGO_BIN_EXE_syftbox-rs");
    let out = Command::new(exe)
        .arg("-c")
        .arg(&cfg_path)
        .arg("login")
        .arg("--quiet")
        .output()
        .expect("run login --quiet");
    assert!(out.status.success());

    assert!(String::from_utf8_lossy(&out.stdout).trim().is_empty());
    assert!(String::from_utf8_lossy(&out.stderr).trim().is_empty());
}
