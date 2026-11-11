# Runs the Worker exactly as built for production, inside workerd (the
# open-source Cloudflare Workers runtime), for end-to-end tests.
# Build context is the repository root.

FROM rust:1-slim AS build
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN cargo install -q "worker-build@^0.8"
WORKDIR /app
COPY rust-toolchain.toml Cargo.toml Cargo.lock ./
COPY crates ./crates
WORKDIR /app/crates/worker
RUN worker-build --release
# The runtime image has no Rust toolchain, so drop the [build] section and
# serve the artifact built above. Everything else matches production config.
RUN awk '/^\[build\]/{skip=1;next} /^\[/{skip=0} !skip' wrangler.toml > wrangler.e2e.toml

FROM node:22-slim
ENV CI=true WRANGLER_SEND_METRICS=false
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g wrangler@4
WORKDIR /worker
COPY --from=build /app/crates/worker/build ./build
COPY --from=build /app/crates/worker/wrangler.e2e.toml ./wrangler.toml
EXPOSE 8787
CMD ["wrangler", "dev", "--ip", "0.0.0.0", "--port", "8787"]
