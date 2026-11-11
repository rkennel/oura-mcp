//! Shared helpers for end-to-end tests.

use std::time::Duration;

use ureq::Agent;

/// Longest a single request may take before the test fails.
const REQUEST_TIMEOUT: Duration = Duration::from_secs(5);

/// What the Worker sent back for one request.
pub struct HttpResponse {
    pub status: u16,
    pub body: String,
}

/// Sends `GET <base url><path>` to the Worker under test.
///
/// HTTP error statuses are returned, not treated as failures, so tests can
/// assert on them. Failing to reach the Worker at all panics with a hint.
pub fn get(path: &str) -> HttpResponse {
    let url = format!("{}{path}", base_url());

    let mut response = agent().get(&url).call().unwrap_or_else(|error| {
        panic!(
            "could not reach the Worker at {url}: {error}\n\
             Is it running? Start it and run these tests with scripts/e2e-test.sh"
        )
    });

    let status = response.status().as_u16();
    let body = response
        .body_mut()
        .read_to_string()
        .unwrap_or_else(|error| panic!("could not read the response body from {url}: {error}"));

    HttpResponse { status, body }
}

/// Address of the Worker under test. Override with `E2E_BASE_URL`.
fn base_url() -> String {
    std::env::var("E2E_BASE_URL").unwrap_or_else(|_| "http://localhost:8787".to_owned())
}

fn agent() -> Agent {
    Agent::config_builder()
        .timeout_global(Some(REQUEST_TIMEOUT))
        .http_status_as_error(false)
        .build()
        .into()
}
