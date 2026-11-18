# Production image for the Oura MCP server. The end-to-end tests run this
# same image.
#
# Base images are pinned by digest so a rebuild uses exactly the image that
# was tested; the tag beside each digest says what it is. The Go version
# matches the toolchain in go.mod. To update, change the tag and replace the
# digest with the one from `docker buildx imagetools inspect <image>:<tag>`.

FROM golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Distroless: no shell or package manager, and a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/server"]
