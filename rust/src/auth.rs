use anyhow::{Context, Result};
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use chrono::{TimeZone, Utc};
use serde::Deserialize;

#[allow(dead_code)]
#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AuthTokenResponse {
    pub access_token: String,
    pub refresh_token: String,
}

#[derive(Debug, Deserialize)]
struct JwtPayload {
    #[serde(default)]
    r#type: String,
    #[serde(default)]
    sub: String,
    #[serde(default)]
    exp: Option<i64>,
}

pub fn validate_token(token: &str, expected_type: &str, email: &str) -> Result<()> {
    let payload = parse_jwt_payload(token)?;
    if payload.r#type != expected_type {
        anyhow::bail!(
            "invalid token type, expected {expected_type}, got {}",
            payload.r#type
        );
    }
    if !payload.sub.is_empty() && payload.sub != email {
        anyhow::bail!(
            "invalid claims: token subject {:?} does not match {:?}",
            payload.sub,
            email
        );
    }
    if let Some(exp) = payload.exp {
        let t = Utc
            .timestamp_opt(exp, 0)
            .single()
            .ok_or_else(|| anyhow::anyhow!("invalid exp"))?;
        if t <= Utc::now() {
            anyhow::bail!("token expired, login again");
        }
    }
    Ok(())
}

pub fn token_subject(token: &str) -> Option<String> {
    let payload = parse_jwt_payload(token).ok()?;
    let sub = payload.sub.trim().to_string();
    if sub.is_empty() {
        None
    } else {
        Some(sub)
    }
}

fn parse_jwt_payload(token: &str) -> Result<JwtPayload> {
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() < 2 {
        anyhow::bail!("invalid token format");
    }
    let payload = URL_SAFE_NO_PAD.decode(parts[1]).context("base64 decode")?;
    let v = serde_json::from_slice::<JwtPayload>(&payload).context("parse jwt payload")?;
    Ok(v)
}

pub async fn request_email_code(
    http: &reqwest::Client,
    server_url: &str,
    email: &str,
) -> Result<()> {
    #[derive(serde::Serialize)]
    struct Req<'a> {
        email: &'a str,
    }
    let url = format!("{}/auth/otp/request", server_url.trim_end_matches('/'));
    let resp = http
        .post(&url)
        .json(&Req { email })
        .send()
        .await
        .context("http post")?;
    if !resp.status().is_success() {
        anyhow::bail!("request email code: http {}", resp.status());
    }
    Ok(())
}

pub fn is_valid_otp(otp: &str) -> bool {
    static PATTERN: once_cell::sync::Lazy<regex::Regex> =
        once_cell::sync::Lazy::new(|| regex::Regex::new(r"^[0-9A-Z]{8}$").unwrap());
    PATTERN.is_match(otp)
}

pub async fn verify_email_code(
    http: &reqwest::Client,
    server_url: &str,
    email: &str,
    code: &str,
) -> Result<AuthTokenResponse> {
    if !is_valid_otp(code) {
        anyhow::bail!("invalid otp");
    }
    #[derive(serde::Serialize)]
    struct Req<'a> {
        email: &'a str,
        code: &'a str,
    }
    let url = format!("{}/auth/otp/verify", server_url.trim_end_matches('/'));
    let resp = http
        .post(&url)
        .json(&Req { email, code })
        .send()
        .await
        .context("http post")?;
    let status = resp.status();
    let bytes = resp.bytes().await.context("read body")?;
    if !status.is_success() {
        anyhow::bail!("verify email code: http {}", status);
    }
    serde_json::from_slice::<AuthTokenResponse>(&bytes).context("parse auth token response")
}

#[allow(dead_code)]
pub async fn refresh_auth_tokens(
    http: &reqwest::Client,
    server_url: &str,
    refresh_token: &str,
) -> Result<AuthTokenResponse> {
    #[derive(serde::Serialize)]
    struct Req<'a> {
        #[serde(rename = "refreshToken")]
        refresh_token: &'a str,
    }

    let url = format!("{}/auth/refresh", server_url.trim_end_matches('/'));
    let resp = http
        .post(&url)
        .json(&Req { refresh_token })
        .send()
        .await
        .context("http post")?;
    let status = resp.status();
    let bytes = resp.bytes().await.context("read body")?;
    if !status.is_success() {
        anyhow::bail!("refresh auth tokens: http {}", status);
    }
    serde_json::from_slice::<AuthTokenResponse>(&bytes).context("parse auth token response")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fake_jwt(payload: &serde_json::Value) -> String {
        let header = serde_json::json!({"alg":"none","typ":"JWT"});
        format!(
            "{}.{}.sig",
            URL_SAFE_NO_PAD.encode(serde_json::to_vec(&header).unwrap()),
            URL_SAFE_NO_PAD.encode(serde_json::to_vec(payload).unwrap())
        )
    }

    #[test]
    fn validate_token_matches_go_rules() {
        let email = "alice@example.com";
        let ok = fake_jwt(&serde_json::json!({"type":"refresh","sub":email}));
        validate_token(&ok, "refresh", email).unwrap();

        let wrong_type = fake_jwt(&serde_json::json!({"type":"access","sub":email}));
        assert!(validate_token(&wrong_type, "refresh", email)
            .unwrap_err()
            .to_string()
            .contains("invalid token type"));

        let wrong_sub = fake_jwt(&serde_json::json!({"type":"refresh","sub":"bob@example.com"}));
        assert!(validate_token(&wrong_sub, "refresh", email)
            .unwrap_err()
            .to_string()
            .contains("does not match"));

        let expired = fake_jwt(&serde_json::json!({"type":"refresh","sub":email,"exp":1}));
        assert!(validate_token(&expired, "refresh", email)
            .unwrap_err()
            .to_string()
            .contains("expired"));
    }

    #[test]
    fn otp_validation_matches_go() {
        assert!(is_valid_otp("ABCD1234"));
        assert!(!is_valid_otp("abcd1234"));
        assert!(!is_valid_otp("ABC123"));
        assert!(!is_valid_otp("ABCD123!"));
    }
}
