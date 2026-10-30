// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var requestID uint64

// formatHeaders renders all HTTP headers as a sorted, single-line string.
func formatHeaders(h http.Header) string {
	if len(h) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(fmt.Sprintf("%s=%q", k, strings.Join(h[k], ",")))
	}
	return b.String()
}

// formatEnvoyHeaders renders all headers Envoy embedded in the ProcessingRequest.
func formatEnvoyHeaders(eh *envoyHttpHeaders) string {
	if eh == nil {
		return "(none)"
	}
	parts := make([]string, 0, len(eh.Headers.Headers))
	for _, hv := range eh.Headers.Headers {
		parts = append(parts, fmt.Sprintf("%s=%q", hv.Key, headerVal(hv)))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// --- Envoy HTTP ext_proc protocol (proto-JSON) types ---

type envoyHeader struct {
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	RawValue string `json:"raw_value,omitempty"` // base64-encoded bytes (snake_case from Envoy)
}

type envoyHeaderMap struct {
	Headers []envoyHeader `json:"headers"`
}

type envoyHttpHeaders struct {
	Headers envoyHeaderMap `json:"headers"`
}

// ProcessingRequest — only requestHeaders is used; Envoy POSTs this JSON body.
// Envoy serializes proto fields in snake_case (not camelCase).
type processingRequest struct {
	RequestHeaders *envoyHttpHeaders `json:"request_headers,omitempty"`
}

// ProcessingResponse — instruct Envoy to continue and add x-route-target header.
// Envoy parses proto-JSON responses in snake_case (same as requests).
type processingResponse struct {
	RequestHeaders *headersResponse `json:"request_headers,omitempty"`
}

type headersResponse struct {
	Response commonResponse `json:"response"`
}

type commonResponse struct {
	Status          string          `json:"status"`
	ClearRouteCache bool            `json:"clear_route_cache,omitempty"`
	HeaderMutation  *headerMutation `json:"header_mutation,omitempty"`
}

type headerMutation struct {
	SetHeaders []headerValueOption `json:"set_headers,omitempty"`
}

type headerValueOption struct {
	Header envoyHeader `json:"header"`
}

// headerVal decodes a header value, preferring raw (base64) over plain text.
func headerVal(h envoyHeader) string {
	if h.RawValue != "" {
		decoded, err := base64.StdEncoding.DecodeString(h.RawValue)
		if err == nil {
			return string(decoded)
		}
	}
	return h.Value
}

func decideHandler(routerURL string, httpc *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		startedAt := time.Now()
		log.Printf("ext-proc-http id=%d incoming method=%s remote=%s path=%s host=%s http_headers=[%s]",
			id, r.Method, r.RemoteAddr, r.URL.Path, r.Host, formatHeaders(r.Header))

		// Parse the ProcessingRequest JSON body sent by Envoy.
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			log.Printf("ext-proc-http id=%d failed to read body: %v", id, err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		log.Printf("ext-proc-http id=%d ENVOY-PROCESSING-REQUEST raw_body=%q", id, string(body))
		var procReq processingRequest
		if err := json.Unmarshal(body, &procReq); err != nil {
			log.Printf("ext-proc-http id=%d failed to parse ProcessingRequest: %v — body: %.200s", id, err, body)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		log.Printf("ext-proc-http id=%d ENVOY-REQUEST-HEADERS [%s]", id, formatEnvoyHeaders(procReq.RequestHeaders))

		// Extract :path and x-route-key from the Envoy request headers block.
		path := "/"
		routeKey := ""
		if procReq.RequestHeaders != nil {
			for _, hv := range procReq.RequestHeaders.Headers.Headers {
				switch strings.ToLower(hv.Key) {
				case ":path":
					path = headerVal(hv)
				case "x-route-key":
					routeKey = headerVal(hv)
				}
			}
		}

		// Call route-decider with path and route_key as query params — matching
		// exactly what route-decider.decide() reads: r.URL.Query().Get("path")
		// and r.URL.Query().Get("route_key").
		u, _ := url.Parse(routerURL)
		q := u.Query()
		q.Set("path", path)
		q.Set("route_key", routeKey)
		u.RawQuery = q.Encode()
		log.Printf("ext-proc-http id=%d path=%q route_key=%q -> %s", id, path, routeKey, u.String())

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
		if err != nil {
			log.Printf("ext-proc-http id=%d failed to build request: %v", id, err)
			sendProcessingResponse(w, id, "service-a", startedAt)
			return
		}

		resp, err := httpc.Do(req)
		if err != nil {
			log.Printf("ext-proc-http id=%d route-decider call failed after %s: %v", id, time.Since(startedAt), err)
			sendProcessingResponse(w, id, "service-a", startedAt)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("ext-proc-http id=%d route-decider returned status=%d after %s", id, resp.StatusCode, time.Since(startedAt))
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		if err != nil {
			log.Printf("ext-proc-http id=%d read body failed: %v", id, err)
			sendProcessingResponse(w, id, "service-a", startedAt)
			return
		}

		target := strings.TrimSpace(string(respBody))
		log.Printf("ext-proc-http id=%d route-decider -> %q duration=%s", id, target, time.Since(startedAt))
		switch target {
		case "service-b", "service-c", "service-a":
		default:
			target = "service-a"
		}
		sendProcessingResponse(w, id, target, startedAt)
	}
}

// sendProcessingResponse writes an Envoy ProcessingResponse JSON telling Envoy
// to set x-route-target and clear the route cache so header-based HTTPRoutes
// (service-b-ext-proc-route, service-c-ext-proc-route) are re-evaluated.
func sendProcessingResponse(w http.ResponseWriter, id uint64, target string, startedAt time.Time) {
	resp := processingResponse{
		RequestHeaders: &headersResponse{
			Response: commonResponse{
				Status:          "CONTINUE_AND_REPLACE",
				ClearRouteCache: true,
				HeaderMutation: &headerMutation{
					SetHeaders: []headerValueOption{
						{Header: envoyHeader{Key: "x-route-target", RawValue: base64.StdEncoding.EncodeToString([]byte(target))}}, // was: Value: target

					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	respJSON, _ := json.Marshal(resp)
	if _, err := w.Write(respJSON); err != nil {
		log.Printf("ext-proc-http id=%d failed to write response: %v", id, err)
		return
	}
	log.Printf("ext-proc-http id=%d RESPONSE target=%q status=200 body=%q total_duration=%s", id, target, string(respJSON), time.Since(startedAt))
}

func main() {
	addr := "0.0.0.0:9000"
	routerURL := os.Getenv("ROUTER_URL")
	if routerURL == "" {
		routerURL = "http://route-decider.virtual.consul:9500/decide"
	}
	routerTimeout := 5000 * time.Millisecond
	if timeoutMs := os.Getenv("ROUTER_TIMEOUT_MS"); timeoutMs != "" {
		parsed, err := strconv.Atoi(timeoutMs)
		if err != nil {
			log.Fatalf("invalid ROUTER_TIMEOUT_MS %q: %v", timeoutMs, err)
		}
		routerTimeout = time.Duration(parsed) * time.Millisecond
	}

	httpc := &http.Client{Timeout: routerTimeout}
	log.Printf("ext-proc-http listening on %s, router_url=%s, timeout=%s", addr, routerURL, routerTimeout)

	http.HandleFunc("/decide", decideHandler(routerURL, httpc))
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("ext-proc-http health method=%s remote=%s", r.Method, r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("listen failed: %v", err)
	}
}
