package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type jsonRPCResponse[T any] struct {
	Result T `json:"result"`
}

type listToolsResult struct {
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
}

func TestToolDescriptionsExplainRelayRouting(t *testing.T) {
	handler := newMCPHTTPHandler(InitMCPServer(NewSearchService(DefaultConfig())))

	resp := postJSONRPC[listToolsResult](t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)

	descriptions := map[string]string{}
	for _, tool := range resp.Result.Tools {
		descriptions[tool.Name] = tool.Description
	}

	if got := descriptions["read_home_timeline"]; got == "" || !containsAll(got, "without Browser Relay", "without requiring an active X tab") {
		t.Fatalf("read_home_timeline description missing routing guidance: %q", got)
	}
	if got := descriptions["search_x"]; got == "" || !containsAll(got, "without Browser Relay", "without requiring an active X tab") {
		t.Fatalf("search_x description missing routing guidance: %q", got)
	}
}

func TestSearchResultJSONUsesEmptyArraysInsteadOfNull(t *testing.T) {
	result := SearchResult{}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal search result: %v", err)
	}

	if bytes.Contains(data, []byte(`"posts":null`)) {
		t.Fatalf("posts should marshal as an empty array, got %s", data)
	}
	if bytes.Contains(data, []byte(`"related_users":null`)) {
		t.Fatalf("related_users should marshal as an empty array, got %s", data)
	}
}

func TestHomeTimelineResultJSONUsesEmptyArraysInsteadOfNull(t *testing.T) {
	result := HomeTimelineResult{}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal home timeline result: %v", err)
	}

	if bytes.Contains(data, []byte(`"posts":null`)) {
		t.Fatalf("posts should marshal as an empty array, got %s", data)
	}
	if bytes.Contains(data, []byte(`"related_users":null`)) {
		t.Fatalf("related_users should marshal as an empty array, got %s", data)
	}
}

func postJSONRPC[T any](t *testing.T, handler http.Handler, body string) jsonRPCResponse[T] {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse[T]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !bytes.Contains([]byte(value), []byte(part)) {
			return false
		}
	}
	return true
}
