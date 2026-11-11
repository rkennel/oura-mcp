//! The Worker is reachable and reports its identity.

mod support;

use support::get;

#[test]
fn health_endpoint_reports_the_server_is_running() {
    let response = get("/health");

    assert_eq!(response.status, 200);
    assert!(
        response.body.starts_with("oura-mcp ") && response.body.ends_with(" is running"),
        "unexpected health body: {}",
        response.body
    );
}

#[test]
fn unknown_path_is_not_found() {
    let response = get("/no-such-path");

    assert_eq!(response.status, 404);
}
