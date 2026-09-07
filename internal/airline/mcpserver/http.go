package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPPath is where the streamable HTTP transport is mounted.
const MCPPath = "/mcp"

// HealthCheck reports whether the server's dependencies are usable.
type HealthCheck func(context.Context) error

// NewHTTPHandler serves the MCP streamable transport at /mcp and a health
// endpoint at /healthz that the container orchestrator can poll.
func NewHTTPHandler(srv *mcp.Server, health HealthCheck) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(MCPPath, mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv }, nil))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		body := map[string]string{"status": "ok", "server": Name, "version": Version}
		code := http.StatusOK
		if health != nil {
			if err := health(ctx); err != nil {
				body["status"] = "unhealthy"
				body["error"] = err.Error()
				code = http.StatusServiceUnavailable
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	})

	return mux
}
