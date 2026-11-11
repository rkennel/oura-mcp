//! Cloudflare Workers adapter: translates HTTP requests into calls on
//! `oura-mcp-core`. Keep this crate thin; logic belongs in the core crate.

use worker::{Context, Env, Request, Response, Result, Router, event};

#[event(fetch)]
async fn fetch(req: Request, env: Env, _ctx: Context) -> Result<Response> {
    Router::new()
        .get("/health", |_, _| {
            Response::ok(oura_mcp_core::health_message())
        })
        .run(req, env)
        .await
}
