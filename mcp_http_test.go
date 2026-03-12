package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPHandlerAcceptsStaleSessionIDOnPost(t *testing.T) {
	service := NewSearchService(DefaultConfig())
	handler := newMCPHTTPHandler(InitMCPServer(service))

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "stale-session")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected stale session POST to avoid 404, got %d with body %q", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}
