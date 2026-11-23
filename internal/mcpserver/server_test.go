package mcpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testVersion = "1.2.3-test"

// connect starts server on one end of an in-memory connection and returns a
// client session on the other, closed when the test ends.
func connect(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestPingRepliesWithItsFixedText(t *testing.T) {
	result, output, err := ping(t.Context(), &mcp.CallToolRequest{}, struct{}{})

	if err != nil {
		t.Fatalf("ping returned an error: %v", err)
	}
	if output != nil {
		t.Errorf("structured output = %v, want none", output)
	}
	if got := textOf(t, result); got != PingReply {
		t.Errorf("reply = %q, want %q", got, PingReply)
	}
}

func TestNewReportsTheServerNameAndVersion(t *testing.T) {
	session := connect(t, New(testVersion))

	info := session.InitializeResult().ServerInfo
	if info.Name != Name || info.Version != testVersion {
		t.Errorf("server info = %s %s, want %s %s", info.Name, info.Version, Name, testVersion)
	}
}

func TestNewOffersOnlyThePingTool(t *testing.T) {
	session := connect(t, New(testVersion))

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Equal(names, []string{"ping"}) {
		t.Errorf("tools = %v, want [ping]", names)
	}
}

func TestNewAnswersAPingCall(t *testing.T) {
	session := connect(t, New(testVersion))

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "ping"})
	if err != nil {
		t.Fatalf("call ping: %v", err)
	}

	if result.IsError {
		t.Fatalf("ping reported a tool error: %v", result.Content)
	}
	if got := textOf(t, result); got != PingReply {
		t.Errorf("reply = %q, want %q", got, PingReply)
	}
}

// postInitialize sends an MCP initialize request to handler as a client
// would over HTTP.
func postInitialize(t *testing.T, handler http.Handler) *http.Response {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func TestNewHTTPHandlerAnswersInitializeWithPlainJSON(t *testing.T) {
	response := postInitialize(t, NewHTTPHandler(New(testVersion)))
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var message struct {
		Result struct {
			ServerInfo mcp.Implementation `json:"serverInfo"`
		} `json:"result"`
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(content, &message); err != nil {
		t.Fatalf("decode %q: %v", content, err)
	}
	if message.Result.ServerInfo.Name != Name {
		t.Errorf("server name = %q, want %q in %s", message.Result.ServerInfo.Name, Name, content)
	}
}

func TestNewHTTPHandlerIsStateless(t *testing.T) {
	response := postInitialize(t, NewHTTPHandler(New(testVersion)))
	defer func() { _ = response.Body.Close() }()

	if id := response.Header.Get("Mcp-Session-Id"); id != "" {
		t.Errorf("Mcp-Session-Id = %q, want none from a stateless server", id)
	}
}

// textOf returns the text of result's single text content.
func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content = %v, want one item", result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}
