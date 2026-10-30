// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
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

// readAndRestoreBody reads the full request body for logging and restores it so
// downstream handlers can read it again.
func readAndRestoreBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("route-decider failed to read request body: %v", err)
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return data
}

func selectTarget(path, routeKey string) string {
	if strings.HasSuffix(path, "/b") || routeKey == "b" {
		return "service-b"
	}
	if strings.HasSuffix(path, "/c") || routeKey == "c" {
		return "service-c"
	}
	return "service-a"
}

func decide(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	reqBody := readAndRestoreBody(r)
	log.Printf(
		"route-decider REQUEST method=%s url=%q proto=%s host=%s remote=%s query=%q headers=[%s] body=%q",
		r.Method, r.URL.String(), r.Proto, r.Host, r.RemoteAddr,
		r.URL.RawQuery, formatHeaders(r.Header), string(reqBody),
	)

	path := r.URL.Query().Get("path")
	routeKey := strings.ToLower(r.URL.Query().Get("route_key"))
	target := selectTarget(path, routeKey)
	log.Printf(
		"route-decider decision path=%q route_key=%q target=%q user_agent=%q",
		path,
		routeKey,
		target,
		r.UserAgent(),
	)
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(target))
	log.Printf(
		"route-decider RESPONSE status=%d resp_headers=[%s] body=%q path=%q target=%q duration=%s",
		http.StatusOK, formatHeaders(w.Header()), target, path, target, time.Since(startedAt),
	)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/decide", decide)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("route-decider health method=%s remote=%s user_agent=%q", r.Method, r.RemoteAddr, r.UserAgent())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":9500"
	log.Printf("route-decider http server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(fmt.Errorf("route-decider failed: %w", err))
	}
}
