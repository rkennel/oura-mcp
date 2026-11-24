# Image for the fake Oura API that the end-to-end environment runs beside
# the server. Built from the repository root (see e2e/compose.yaml), with
# Dockerfile.dockerignore beside this file choosing what it may copy.
#
# The base images are the production Dockerfile's, pinned by the same
# digests; update both files together.

FROM golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# The fake reuses the server's serving lifecycle from internal/app.
COPY internal ./internal
COPY e2e/fakeoura ./e2e/fakeoura
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fakeoura ./e2e/fakeoura/cmd/fakeoura

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/fakeoura /fakeoura
EXPOSE 8081
USER nonroot
ENTRYPOINT ["/fakeoura"]
