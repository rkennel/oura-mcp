//! Platform-independent logic for the Oura MCP server.
//!
//! Everything in this crate is plain Rust with no Cloudflare dependencies, so
//! it can be unit tested natively. The `oura-mcp-worker` crate adapts it to
//! the Workers runtime.

/// Name the server reports to MCP clients.
pub const SERVER_NAME: &str = "oura-mcp";

/// Version the server reports to MCP clients.
pub const SERVER_VERSION: &str = env!("CARGO_PKG_VERSION");

/// One-line status text for the health endpoint.
#[must_use]
pub fn health_message() -> String {
    format!("{SERVER_NAME} {SERVER_VERSION} is running")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn health_message_names_the_server_and_version() {
        assert_eq!(
            health_message(),
            format!("oura-mcp {} is running", env!("CARGO_PKG_VERSION"))
        );
    }
}
