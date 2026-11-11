# Serves the production build artifact of the Worker inside workerd (the
# open-source Cloudflare Workers runtime) for end-to-end tests. This is a
# local simulation: storage, secrets and Cloudflare Access are emulated or
# absent. Build context is the repository root.

FROM rust:1-slim AS build
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl pkg-config libssl-dev \
    && rm -rf /var/lib/apt/lists/*
# Same version as the [build] command in crates/worker/wrangler.toml.
RUN cargo install -q --locked "worker-build@0.8.7"
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
    && rm -rf /var/lib/apt/lists/*
WORKDIR /worker
# wrangler comes from the lock file, so tests and deploys use one version.
COPY crates/worker/package.json crates/worker/package-lock.json ./
RUN npm ci
COPY --from=build /app/crates/worker/build ./build
COPY --from=build /app/crates/worker/wrangler.e2e.toml ./wrangler.toml
EXPOSE 8787
CMD ["npx", "wrangler", "dev", "--ip", "0.0.0.0", "--port", "8787"]
