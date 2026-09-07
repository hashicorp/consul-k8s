// Package main implements "ameduss", a mock MCP (Model Context Protocol)
// server for flight and hotel booking. It exposes four tools:
//   - hotel.search
//   - hotel.book
//   - flight.search
//   - flight.book
//
// Every tool returns a fixed, deterministic mock response so the server can be
// used for local development and integration testing without any real backend.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "ameduss"
	serverVersion = "1.0.0"
)

func main() {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(true),
		server.WithLogging(),
	)

	registerHotelTools(s)
	registerFlightTools(s)

	// Transport selection:
	//   MCP_TRANSPORT=http  -> Streamable HTTP server (used by Docker).
	//   MCP_TRANSPORT=stdio -> stdio server (default, for MCP clients).
	transport := strings.ToLower(strings.TrimSpace(os.Getenv("MCP_TRANSPORT")))
	if transport == "" {
		transport = "stdio"
	}

	switch transport {
	case "http", "streamable", "streamable-http":
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
	default:
		// Serve over stdio so the server can be driven by any MCP client.
		if err := server.ServeStdio(s); err != nil {
			fmt.Printf("server error: %v\n", err)
		}
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


func registerHotelTools(s *server.MCPServer) {
	// hotel.search
	s.AddTool(mcp.NewTool("hotel.search",
		mcp.WithDescription("Search for available hotels for a location and date range."),
		mcp.WithString("location", mcp.Description("City or destination to search hotels in.")),
		mcp.WithString("check_in", mcp.Description("Check-in date (YYYY-MM-DD).")),
		mcp.WithString("check_out", mcp.Description("Check-out date (YYYY-MM-DD).")),
		mcp.WithNumber("guests", mcp.Description("Number of guests.")),
	), handleHotelSearch)

	// hotel.book
	s.AddTool(mcp.NewTool("hotel.book",
		mcp.WithDescription("Book a hotel by its identifier."),
		mcp.WithString("hotel_id", mcp.Required(), mcp.Description("Identifier of the hotel to book.")),
		mcp.WithString("check_in", mcp.Description("Check-in date (YYYY-MM-DD).")),
		mcp.WithString("check_out", mcp.Description("Check-out date (YYYY-MM-DD).")),
		mcp.WithString("guest_name", mcp.Description("Name of the primary guest.")),
	), handleHotelBook)
}

func registerFlightTools(s *server.MCPServer) {
	// flight.search
	s.AddTool(mcp.NewTool("flight.search",
		mcp.WithDescription("Search for available flights between two airports."),
		mcp.WithString("origin", mcp.Description("Origin airport code, e.g. SFO.")),
		mcp.WithString("destination", mcp.Description("Destination airport code, e.g. JFK.")),
		mcp.WithString("date", mcp.Description("Departure date (YYYY-MM-DD).")),
		mcp.WithNumber("passengers", mcp.Description("Number of passengers.")),
	), handleFlightSearch)

	// flight.book
	s.AddTool(mcp.NewTool("flight.book",
		mcp.WithDescription("Book a flight by its identifier."),
		mcp.WithString("flight_id", mcp.Required(), mcp.Description("Identifier of the flight to book.")),
		mcp.WithString("passenger_name", mcp.Description("Name of the passenger.")),
		mcp.WithString("seat_class", mcp.Description("Seat class, e.g. economy or business.")),
	), handleFlightBook)
}

func handleHotelSearch(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"status": "ok",
		"count":  2,
		"hotels": []map[string]any{
			{
				"hotel_id":        "HTL-1001",
				"name":            "Grand Aurora Hotel",
				"location":        "San Francisco, CA",
				"rating":          4.6,
				"price_per_night": 189.00,
				"currency":        "USD",
				"available":       true,
			},
			{
				"hotel_id":        "HTL-1002",
				"name":            "Bayview Suites",
				"location":        "San Francisco, CA",
				"rating":          4.2,
				"price_per_night": 145.50,
				"currency":        "USD",
				"available":       true,
			},
		},
	}), nil
}

func handleHotelBook(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"status":            "confirmed",
		"confirmation_code": "HB-77A3C9",
		"hotel_id":          "HTL-1001",
		"hotel_name":        "Grand Aurora Hotel",
		"check_in":          "2026-08-01",
		"check_out":         "2026-08-04",
		"nights":            3,
		"total_price":       567.00,
		"currency":          "USD",
	}), nil
}

func handleFlightSearch(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"status": "ok",
		"count":  2,
		"flights": []map[string]any{
			{
				"flight_id":   "FL-2201",
				"airline":     "Ameduss Air",
				"origin":      "SFO",
				"destination": "JFK",
				"departure":   "2026-08-01T08:30:00Z",
				"arrival":     "2026-08-01T17:00:00Z",
				"price":       320.00,
				"currency":    "USD",
				"seats_left":  12,
			},
			{
				"flight_id":   "FL-2202",
				"airline":     "Ameduss Air",
				"origin":      "SFO",
				"destination": "JFK",
				"departure":   "2026-08-01T14:15:00Z",
				"arrival":     "2026-08-01T22:45:00Z",
				"price":       275.00,
				"currency":    "USD",
				"seats_left":  4,
			},
		},
	}), nil
}

func handleFlightBook(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"status":            "confirmed",
		"confirmation_code": "FB-4521BD",
		"flight_id":         "FL-2201",
		"airline":           "Ameduss Air",
		"origin":            "SFO",
		"destination":       "JFK",
		"seat":              "14C",
		"seat_class":        "economy",
		"total_price":       320.00,
		"currency":          "USD",
	}), nil
}
