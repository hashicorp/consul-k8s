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

// service-d is a simple edge proxy. Every request received from the api-gateway
// is forwarded as-is to service-e over the service mesh (transparent proxy).
// service-d makes no routing decision of its own; the dynamic routing happens
// at service-e via ext-proc.
func upstream() string {
	if u := os.Getenv("UPSTREAM_E"); u != "" {
		return u
	}
	return "http://service-e.virtual.consul:8080"
}

func proxy(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()

	// Read and restore the incoming body so we can both log it and forward it.
	reqBody, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	targetURL := upstream() + r.URL.RequestURI()
	log.Printf(
		"service-d REQUEST method=%s url=%q proto=%s host=%s remote=%s headers=[%s] body=%q upstream=%q",
		r.Method, r.URL.String(), r.Proto, r.Host, r.RemoteAddr,
		formatHeaders(r.Header), string(reqBody), targetURL,
	)

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("service-d proxy build request error: %v", err)
		http.Error(w, "upstream request build failed", http.StatusBadGateway)
		return
	}
	for _, h := range []string{"Content-Type", "Accept", "User-Agent", "x-route-key"} {
		if v := r.Header.Get(h); v != "" {
			outReq.Header.Set(h, v)
		}
	}
	outReq.Header.Set("x-via-service-d", "true")
	log.Printf("service-d UPSTREAM-REQUEST method=%s url=%q headers=[%s]",
		outReq.Method, targetURL, formatHeaders(outReq.Header))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		log.Printf("service-d proxy upstream call failed after %s: %v", time.Since(startedAt), err)
		http.Error(w, "upstream call failed", http.StatusBadGateway)
		return
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	log.Printf("service-d UPSTREAM-RESPONSE status=%d resp_headers=[%s] body=%q",
		resp.StatusCode, formatHeaders(resp.Header), string(respBody))

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("x-served-by", "service-d")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, bytes.NewReader(respBody)); err != nil {
		log.Printf("service-d proxy write response body error: %v", err)
	}
	log.Printf("service-d RESPONSE path=%q status=%d resp_headers=[%s] body=%q duration=%s",
		r.URL.Path, resp.StatusCode, formatHeaders(w.Header()), string(respBody), time.Since(startedAt))
}

func main() {
	addr := ":8080"
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("service-d health method=%s remote=%s", r.Method, r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", proxy)

	log.Printf("service-d proxy listening on %s, forwarding to %s", addr, upstream())
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(fmt.Errorf("service-d failed: %w", err))
	}
}
