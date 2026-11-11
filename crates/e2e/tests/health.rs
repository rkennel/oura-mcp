//! The Worker is reachable and reports its identity.

mod support;

use support::base_url;

#[test]
fn health_endpoint_reports_the_server_is_running() {
    let body = ureq::get(&format!("{}/health", base_url()))
        .call()
        .expect("health request should succeed")
        .into_string()
        .expect("health body should be text");

    assert!(
        body.starts_with("oura-mcp ") && body.ends_with(" is running"),
        "unexpected health body: {body}"
    );
}

#[test]
fn unknown_path_is_not_found() {
    let result = ureq::get(&format!("{}/no-such-path", base_url())).call();

    match result {
        Err(ureq::Error::Status(status, _)) => assert_eq!(status, 404),
        other => panic!("expected 404, got {other:?}"),
    }
}
