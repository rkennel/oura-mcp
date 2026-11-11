//! Shared helpers for end-to-end tests.

/// Address of the Worker under test. Override with `E2E_BASE_URL`.
pub fn base_url() -> String {
    std::env::var("E2E_BASE_URL").unwrap_or_else(|_| "http://localhost:8787".to_owned())
}
