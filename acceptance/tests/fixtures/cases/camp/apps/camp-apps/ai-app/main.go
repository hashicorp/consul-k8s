// ai-app is a mock AI agent entrypoint for the CAMP mesh.
//
// In the full flow a request to ai-app would be sent out through the local
// Connect sidecar (localhost:15001) and on to the ext-proc / HITL / MCP hops.
// For this task ai-app exercises the deployed MCP servers (weatherly + ameduss)
// by POSTing a single MCP JSON-RPC `tools/call` per request, picking one tool at
// random from the full set, to an upstream over HTTP on 127.0.0.1:15101/mcp (the
// mesh MCP gateway). The CAMP `mcp` filter parses the tool name and routes the
// call to the owning backend. ai-app listens on :8080 and logs verbosely.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// mcpTool describes one MCP `tools/call` the agent can exercise. Name is the
// gateway-visible, server-prefixed tool name (`<server>__<tool>`) that the CAMP
// mcp_router uses to route the call to the owning backend.
type mcpTool struct {
	Name string
	Args map[string]any
}

// mcpTools is the full set of tools across every deployed MCP server
// (weatherly + ameduss). ai-app picks ONE of them at random per invocation, so
// only a single service/tool is called at a time.
var mcpTools = []mcpTool{
	{Name: "weatherly__weather.current", Args: map[string]any{"location": "San Francisco"}},
	{Name: "weatherly__weather.forecast", Args: map[string]any{"location": "San Francisco", "days": 3}},
	{Name: "weatherly__weather.alerts", Args: map[string]any{"location": "San Francisco"}},
	{Name: "ameduss__hotel.search", Args: map[string]any{"location": "San Francisco", "check_in": "2026-08-01", "check_out": "2026-08-04", "guests": 2}},
	{Name: "ameduss__hotel.book", Args: map[string]any{"hotel_id": "HTL-1001", "check_in": "2026-08-01", "check_out": "2026-08-04", "guest_name": "Ada Lovelace"}},
	{Name: "ameduss__flight.search", Args: map[string]any{"origin": "SFO", "destination": "JFK", "date": "2026-08-01", "passengers": 1}},
	{Name: "ameduss__flight.book", Args: map[string]any{"flight_id": "FL-2201", "passenger_name": "Ada Lovelace", "seat_class": "economy"}},
}

// toolResult captures the outcome of a single MCP tools/call.
type toolResult struct {
	Tool   string
	Status int
	Body   string
	Err    error
}

// summary renders a human-readable result, including the upstream response
// body, for logs / the HTTP body.
func (t toolResult) summary() string {
	if t.Err != nil {
		return fmt.Sprintf("  %-28s unreachable (%v)", t.Tool, t.Err)
	}
	body := strings.TrimSpace(t.Body)
	if body == "" {
		body = "(empty)"
	}
	return fmt.Sprintf("  %-28s HTTP %d (%d bytes)\n  response: %s", t.Tool, t.Status, len(t.Body), body)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("[*****ai-app*****] ")

	addr := envOr("AIAPP_ADDR", ":8080")
	upstream := envOr("AIAPP_UPSTREAM", "http://127.0.0.1:15101/mcp")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("healthz from %s", r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// ai-mcp: primary MCP entrypoint (moved from root). Supports:
	//  - GET/POST /ai-mcp/tools/list -> aggregated tools/list via mesh gateway
	//  - GET/POST /ai-mcp/<server>__<tool> -> call the named tool
	aiHandler := func(w http.ResponseWriter, r *http.Request) {
		log.Printf("--> %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		// Strip the /ai-mcp prefix and treat the remainder as the tool name.
		path := strings.TrimPrefix(r.URL.Path, "/ai-mcp")
		name := strings.Trim(path, "/")

		if name == "tools/list" || name == "list" {
			status, body, err := callToolsList(upstream)
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusBadGateway)
				fmt.Fprintf(w, "ai-app: tools/list unreachable via %s: %v\n", upstream, err)
				log.Printf("<-- 502 tools/list via %s: %v", upstream, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			log.Printf("<-- %d tools/list via %s (%d bytes)", status, upstream, len(body))
			return
		}

		var res toolResult
		if name == "" {
			res = exerciseOneTool(upstream)
		} else {
			res = callNamedTool(upstream, name)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ai-app OK\ncalled 1 MCP tool via %s:\n%s\n", upstream, res.summary())
		log.Printf("<-- 200 called %s via %s", res.Tool, upstream)
	}

	mux.HandleFunc("/ai-mcp", aiHandler)
	mux.HandleFunc("/ai-mcp/", aiHandler)

	// Root: static plain-text API and examples.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ai-app\n\nAvailable endpoints:\n  /ai-mcp/tools/list -> aggregated tools/list via mesh MCP gateway\n  /ai-mcp/<server>__<tool> -> call specific tool (e.g. /ai-mcp/ameduss__hotel.search)\n  /healthz -> health check\n"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           logHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("starting ai-app on %s", addr)
	log.Printf("configured upstream: %s", upstream)

	// Call one random tool once at startup so the logs show intent even without
	// external traffic.
	go func() {
		time.Sleep(2 * time.Second)
		res := exerciseOneTool(upstream)
		if res.Err != nil {
			log.Printf("startup probe: %s unreachable: %v", res.Tool, res.Err)
		} else {
			log.Printf("startup probe: %s -> HTTP %d\n%s", res.Tool, res.Status, res.summary())
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

// callTool POSTs an MCP JSON-RPC 2.0 `tools/call` for the given tool to the
// configured upstream and returns its status code and (truncated) body. The MCP
// Streamable HTTP gateway (127.0.0.1:15101) routes it to the owning backend
// based on the server-prefixed tool name.
func callTool(url string, t mcpTool) (int, string, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": t.Name, "arguments": t.Args},
	})
	if err != nil {
		return 0, "", err
	}

	log.Printf("dialing upstream %s (MCP tools/call %s) ...", url, t.Name)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// MCP Streamable HTTP clients must accept both JSON and SSE responses.
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	// Log every header received in the upstream HTTP response.
	log.Printf("upstream response %s -> HTTP %d, %d header(s):", url, resp.StatusCode, len(resp.Header))
	for name, vals := range resp.Header {
		for _, v := range vals {
			log.Printf("    resp header %s: %s", name, v)
		}
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return resp.StatusCode, string(body), nil
}

// exerciseOneTool picks a single tool at random from mcpTools and calls it, so
// only one service/tool is exercised per invocation.
func exerciseOneTool(url string) toolResult {
	t := mcpTools[rand.Intn(len(mcpTools))]
	status, body, err := callTool(url, t)
	return toolResult{Tool: t.Name, Status: status, Body: body, Err: err}
}

// callToolsList POSTs an MCP JSON-RPC 2.0 `tools/list` to the configured
// upstream (the mesh MCP gateway) and returns its status code and body. The
// gateway aggregates tools across every backend MCP server, so the result lists
// all available entries with server-prefixed names (`<server>__<tool>`). The
// Streamable HTTP response may be SSE-framed, so the body is unwrapped.
func callToolsList(url string) (int, string, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if err != nil {
		return 0, "", err
	}

	log.Printf("dialing upstream %s (MCP tools/list) ...", url)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	log.Printf("upstream response %s -> HTTP %d, %d header(s):", url, resp.StatusCode, len(resp.Header))
	for name, vals := range resp.Header {
		for _, v := range vals {
			log.Printf("    resp header %s: %s", name, v)
		}
	}

	// tools/list is larger than a single tool call; allow up to 64 KiB.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, unwrapSSE(string(raw)), nil
}

// unwrapSSE strips the Streamable-HTTP SSE framing ("event: message\ndata:
// {json}") when present, returning the concatenated JSON payload; otherwise it
// returns the input trimmed.
func unwrapSSE(s string) string {
	if strings.Contains(s, "data:") {
		var b strings.Builder
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "data:") {
				b.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return strings.TrimSpace(s)
}

// callNamedTool calls the tool whose server-prefixed name matches `name`
// (as taken from the request path, e.g. "ameduss__flight.book"). If the name
// matches a predefined tool its canned arguments are used; otherwise the tool
// is called with no arguments so any "<server>__<tool>" name still routes to
// its backend via the upstream mcp_router.
func callNamedTool(url, name string) toolResult {
	for _, t := range mcpTools {
		if t.Name == name {
			status, body, err := callTool(url, t)
			return toolResult{Tool: t.Name, Status: status, Body: body, Err: err}
		}
	}
	t := mcpTool{Name: name}
	status, body, err := callTool(url, t)
	return toolResult{Tool: name, Status: status, Body: body, Err: err}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loggingResponseWriter captures the status code so it can be logged after the
// handler runs (response headers are read back from Header()).
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (l *loggingResponseWriter) WriteHeader(code int) {
	l.status = code
	l.ResponseWriter.WriteHeader(code)
}

// logHeaders logs every request header received, and every response header sent
// back, for each request.
func logHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, vals := range r.Header {
			for _, v := range vals {
				log.Printf("    req  header %s: %s", name, v)
			}
		}
		lw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		for name, vals := range lw.Header() {
			for _, v := range vals {
				log.Printf("    resp header %s: %s", name, v)
			}
		}
	})
}
