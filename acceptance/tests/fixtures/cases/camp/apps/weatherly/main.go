// Package main implements "weatherly", a mock MCP (Model Context Protocol)
// server that provides weather information. It exposes three tools:
//   - weather.current
//   - weather.forecast
//   - weather.alerts
//
// Every tool returns a fixed, deterministic mock response so the server can be
// used for local development and integration testing without any real backend.
//
// This server supports the Streamable HTTP transport only.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "weatherly"
	serverVersion = "1.0.0"
)

func main() {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(true),
		server.WithLogging(),
	)

	registerWeatherTools(s)

	// Streamable HTTP transport only.
	addr := os.Getenv("MCP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	// Stateless so a `tools/call` routed here by the CAMP mcp_router works
	// without a prior initialize/session handshake.
	httpServer := server.NewStreamableHTTPServer(s, server.WithStateLess(true))
	log.Printf("%s %s listening on %s (endpoint: /mcp)", serverName, serverVersion, addr)
	if err := http.ListenAndServe(addr, logHeaders(httpServer)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// jsonResult marshals v into an indented JSON string and wraps it in an MCP
// tool result.
func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to encode response: %v", err))
	}
	return mcp.NewToolResultText(string(b))
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
// back, for each request handled by the MCP HTTP server.
func logHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("--> %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
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
		log.Printf("<-- %d %s %s", lw.status, r.Method, r.URL.Path)
	})
}

func registerWeatherTools(s *server.MCPServer) {
	// weather.current
	s.AddTool(mcp.NewTool("weather.current",
		mcp.WithDescription("Get the current weather conditions for a location."),
		mcp.WithString("location", mcp.Required(), mcp.Description("City or location name, e.g. 'San Francisco'.")),
		mcp.WithString("units", mcp.Description("Unit system: 'metric' or 'imperial'. Defaults to metric.")),
	), handleWeatherCurrent)

	// weather.forecast
	s.AddTool(mcp.NewTool("weather.forecast",
		mcp.WithDescription("Get a multi-day weather forecast for a location."),
		mcp.WithString("location", mcp.Required(), mcp.Description("City or location name, e.g. 'San Francisco'.")),
		mcp.WithNumber("days", mcp.Description("Number of forecast days (1-7). Defaults to 3.")),
		mcp.WithString("units", mcp.Description("Unit system: 'metric' or 'imperial'. Defaults to metric.")),
	), handleWeatherForecast)

	// weather.alerts
	s.AddTool(mcp.NewTool("weather.alerts",
		mcp.WithDescription("Get active severe-weather alerts for a location."),
		mcp.WithString("location", mcp.Required(), mcp.Description("City or location name, e.g. 'San Francisco'.")),
	), handleWeatherAlerts)
}

func handleWeatherCurrent(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"status":       "ok",
		"location":     "San Francisco, CA",
		"observed_at":  "2026-07-08T14:00:00Z",
		"units":        "metric",
		"temperature":  19.5,
		"feels_like":   18.9,
		"humidity":     72,
		"wind_speed":   14.0,
		"wind_dir":     "W",
		"condition":    "Partly cloudy",
		"condition_id": "partly_cloudy",
		"uv_index":     5,
	}), nil
}

func handleWeatherForecast(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"status":   "ok",
		"location": "San Francisco, CA",
		"units":    "metric",
		"days":     3,
		"forecast": []map[string]any{
			{
				"date":       "2026-07-08",
				"high":       21.0,
				"low":        14.0,
				"condition":  "Partly cloudy",
				"precip_mm":  0.0,
				"precip_pct": 10,
			},
			{
				"date":       "2026-07-09",
				"high":       23.5,
				"low":        15.0,
				"condition":  "Sunny",
				"precip_mm":  0.0,
				"precip_pct": 0,
			},
			{
				"date":       "2026-07-10",
				"high":       20.0,
				"low":        13.5,
				"condition":  "Light rain",
				"precip_mm":  4.2,
				"precip_pct": 65,
			},
		},
	}), nil
}

func handleWeatherAlerts(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"status":   "ok",
		"location": "San Francisco, CA",
		"count":    1,
		"alerts": []map[string]any{
			{
				"alert_id":  "WX-ALRT-3391",
				"event":     "Heat Advisory",
				"severity":  "moderate",
				"headline":  "Heat Advisory in effect from 12 PM to 8 PM",
				"starts_at": "2026-07-09T19:00:00Z",
				"ends_at":   "2026-07-10T03:00:00Z",
				"areas":     []string{"San Francisco", "Alameda", "Contra Costa"},
			},
		},
	}), nil
}
