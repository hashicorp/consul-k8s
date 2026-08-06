// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// formatHeaders renders all HTTP headers as a sorted, single-line string for
// extensive request/response logging.
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

// service-e is a microservice sitting behind a connect-proxy that has ext-proc
// configured on its inbound listener. By the time a request reaches this app,
// the ext-proc filter (ext-proc-connect-proxy) has already consulted
// http-decider-connect-proxy and stamped an x-route-target header of either
// "service-f" or "service-g". service-e reads that header and proxies the
// request to the chosen upstream over the service mesh (transparent proxy).
//
// Fallback behaviour: if the x-route-target header is missing/unknown, or if the
// chosen upstream call fails, the request is routed to service-a instead and its
// response is rendered to the caller.
func upstreamA() string {
	if v := os.Getenv("UPSTREAM_A"); v != "" {
		return v
	}
	return "http://service-a.virtual.consul:8080"
}

// upstreamFor resolves the chosen upstream from the x-route-target header.
// The bool return is false when the header is missing or unknown, signalling
// the caller to fall back to service-a.
func upstreamFor(routeTarget string) (string, string, bool) {
	upstreamF := os.Getenv("UPSTREAM_F")
	if upstreamF == "" {
		upstreamF = "http://service-f.virtual.consul:8080"
	}
	upstreamG := os.Getenv("UPSTREAM_G")
	if upstreamG == "" {
		upstreamG = "http://service-g.virtual.consul:8080"
	}

	switch routeTarget {
	case "service-g":
		return upstreamG, "service-g", true
	case "service-f":
		return upstreamF, "service-f", true
	default:
		// No/unknown header: fall back to service-a.
		return upstreamA(), "service-a", false
	}
}

// callUpstream performs the proxied request and returns the upstream response,
// its fully-read body and the resolved status code.
func callUpstream(r *http.Request, reqBody []byte, base, routeTarget string) (*http.Response, []byte, error) {
	targetURL := base + r.URL.RequestURI()
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	for _, h := range []string{"Content-Type", "Accept", "User-Agent", "x-route-key"} {
		if v := r.Header.Get(h); v != "" {
			outReq.Header.Set(h, v)
		}
	}
	outReq.Header.Set("x-route-target", routeTarget)
	outReq.Header.Set("x-via-service-e", "true")
	log.Printf("service-e UPSTREAM-REQUEST method=%s url=%q headers=[%s] body=%q",
		outReq.Method, targetURL, formatHeaders(outReq.Header), string(reqBody))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		return nil, nil, fmt.Errorf("call %s: %w", targetURL, err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return resp, nil, fmt.Errorf("read body from %s: %w", targetURL, readErr)
	}
	log.Printf("service-e UPSTREAM-RESPONSE url=%q status=%d resp_headers=[%s] body=%q",
		targetURL, resp.StatusCode, formatHeaders(resp.Header), string(body))
	return resp, body, nil
}

func proxy(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()

	// Read and restore the incoming body so we can both log it and forward it.
	reqBody, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	routeTarget := r.Header.Get("x-route-target")
	upstream, resolved, headerOK := upstreamFor(routeTarget)

	log.Printf(
		"service-e REQUEST method=%s url=%q proto=%s host=%s remote=%s x-route-target=%q headers=[%s] body=%q",
		r.Method, r.URL.String(), r.Proto, r.Host, r.RemoteAddr,
		routeTarget, formatHeaders(r.Header), string(reqBody),
	)

	if !headerOK {
		log.Printf(
			"service-e routing x-route-target=%q MISSING/UNKNOWN -> falling back to service-a (%s)",
			routeTarget, upstream,
		)
	} else {
		log.Printf(
			"service-e routing x-route-target=%q resolved=%q upstream=%q",
			routeTarget, resolved, upstream,
		)
	}

	resp, body, err := callUpstream(r, reqBody, upstream, routeTarget)
	// Treat both transport errors and 5xx upstream responses (e.g. Envoy's
	// "503 no healthy upstream" when the target has no endpoints) as a failure
	// that triggers the service-a fallback.
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		if err != nil {
			log.Printf("service-e proxy upstream=%q FAILED after %s: %v -> falling back to service-a",
				resolved, time.Since(startedAt), err)
		} else {
			log.Printf("service-e proxy upstream=%q returned status=%d after %s -> falling back to service-a",
				resolved, resp.StatusCode, time.Since(startedAt))
		}
		fallback := upstreamA()
		resolved = "service-a"
		resp, body, err = callUpstream(r, reqBody, fallback, "service-a")
		if err != nil {
			log.Printf("service-e proxy service-a fallback FAILED after %s: %v", time.Since(startedAt), err)
			http.Error(w, "upstream and fallback call failed", http.StatusBadGateway)
			return
		}
	}

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("x-served-by", "service-e")
	w.Header().Set("x-routed-to", resolved)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, bytes.NewReader(body)); err != nil {
		log.Printf("service-e proxy write response body error: %v", err)
	}
	log.Printf("service-e RESPONSE path=%q resolved=%q status=%d resp_headers=[%s] body=%q duration=%s",
		r.URL.Path, resolved, resp.StatusCode, formatHeaders(w.Header()), string(body), time.Since(startedAt))
}

func main() {
	addr := ":8080"
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("service-e health method=%s remote=%s", r.Method, r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", proxy)

	log.Printf("service-e proxy listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(fmt.Errorf("service-e failed: %w", err))
	}
}
