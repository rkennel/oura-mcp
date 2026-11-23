//go:build e2e

package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// imageVersion is the version e2e/compose.yaml builds the image with.
const imageVersion = "e2e"

// keyTransport sends every request with key in the x-api-key header.
type keyTransport struct {
	key  string
	next http.RoundTripper
}

func (t keyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("x-api-key", t.key)
	return t.next.RoundTrip(request) //nolint:wrapcheck // a RoundTripper passes errors through unchanged.
}

// connectMCP connects a real MCP client to the server under test with the
// API key, as Claude does. The session is closed when the test ends.
func connectMCP(t *testing.T) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.StreamableClientTransport{
		Endpoint: baseURL() + "/mcp",
		HTTPClient: &http.Client{
			Timeout:   requestTimeout,
			Transport: keyTransport{key: apiKey(), next: http.DefaultTransport},
		},
		MaxRetries: -1,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "oura-mcp-e2e", Version: "0"}, nil)
	session, err := client.Connect(t.Context(), transport, nil)
	if err != nil {
		t.Fatalf("connect to %s: %v", transport.Endpoint, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestMCPInitializeReportsTheServerNameAndVersion(t *testing.T) {
	session := connectMCP(t)

	info := session.InitializeResult().ServerInfo
	if info.Name != "oura-mcp" || info.Version != imageVersion {
		t.Errorf("server info = %s %s, want oura-mcp %s", info.Name, info.Version, imageVersion)
	}
}

func TestMCPListsThePingTool(t *testing.T) {
	session := connectMCP(t)

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	listed := slices.ContainsFunc(tools.Tools, func(tool *mcp.Tool) bool { return tool.Name == "ping" })
	if !listed {
		t.Errorf("tools/list did not include ping: %v", tools.Tools)
	}
}

func TestMCPPingReturnsItsFixedReply(t *testing.T) {
	session := connectMCP(t)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "ping"})
	if err != nil {
		t.Fatalf("call ping: %v", err)
	}

	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("ping result = %+v, want one text reply", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "pong" {
		t.Errorf("ping replied %+v, want the text %q", result.Content[0], "pong")
	}
}

// mcpCallsWithoutAKey are the JSON-RPC requests an MCP client makes to
// initialize, list tools and call ping.
var mcpCallsWithoutAKey = map[string]string{
	"initialize": `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"oura-mcp-e2e","version":"0"}}}`,
	"tools/list": `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	"tools/call": `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}`,
}

func TestMCPCallsWithoutTheKeyAreUnauthorized(t *testing.T) {
	for method, body := range mcpCallsWithoutAKey {
		t.Run(method, func(t *testing.T) {
			status := postMCPWithoutKey(t, body)

			if status != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", status, http.StatusUnauthorized)
			}
		})
	}
}

// postMCPWithoutKey posts body to the MCP endpoint the way an MCP client
// does, but with no x-api-key header, and returns the response status.
func postMCPWithoutKey(t *testing.T, body string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL()+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer closeBody(t, response)
	return response.StatusCode
}
