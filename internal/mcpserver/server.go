// Package mcpserver defines the MCP server: its identity, its tools and the
// HTTP transport Claude reaches it through.
package mcpserver

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// Name is the server name reported in the MCP handshake.
	Name = "oura-mcp"

	// PingReply is the fixed text the ping tool answers with.
	PingReply = "pong"
)

// pingTool describes the placeholder tool that proves a client can reach the
// server and call a tool, before any Oura data is exposed.
var pingTool = &mcp.Tool{
	Name:        "ping",
	Description: "Checks that the Oura MCP server is reachable. Replies with \"" + PingReply + "\".",
	Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
}

// New returns an MCP server that reports Name and version in the handshake
// and offers the ping tool.
func New(version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: Name, Version: version}, nil)
	mcp.AddTool(server, pingTool, ping)
	return server
}

// NewHTTPHandler serves server over the streamable HTTP transport. It is
// stateless, so every request stands alone and any instance can answer it,
// and it answers with plain JSON rather than an event stream, because no
// tool sends progress or server-initiated messages.
func NewHTTPHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}

// ping answers every call with PingReply.
func ping(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: PingReply}},
	}, nil, nil
}
