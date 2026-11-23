package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Gatumbac/nand"
)

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "missing base url", cfg: Config{APIKey: "k", Model: "m"}},
		{name: "missing api key", cfg: Config{BaseURL: "https://example.com", Model: "m"}},
		{name: "missing model", cfg: Config{BaseURL: "https://example.com", APIKey: "k"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestCompleteSuccess(t *testing.T) {
	var capturedAuth string
	var capturedReq chatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&capturedReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"finish_reason": "stop",
				"message": {"role": "assistant", "content": "nand works"}
			}],
			"usage": {"prompt_tokens": 12, "completion_tokens": 3, "total_tokens": 15}
		}`))
	}))
	defer server.Close()

	client := mustNewClient(t, server.URL, "secret-key", "gemini-3.6-flash")
	resp, err := client.Complete(context.Background(), nand.CompletionRequest{
		System:   "Be brief.",
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if capturedAuth != "Bearer secret-key" {
		t.Errorf("got auth %q, want %q", capturedAuth, "Bearer secret-key")
	}
	if capturedReq.Model != "gemini-3.6-flash" {
		t.Errorf("got model %q, want %q", capturedReq.Model, "gemini-3.6-flash")
	}
	if len(capturedReq.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(capturedReq.Messages))
	}
	if resp.Message.Content != "nand works" {
		t.Errorf("got content %q, want %q", resp.Message.Content, "nand works")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("got finish reason %q, want %q", resp.FinishReason, "stop")
	}
	wantUsage := nand.Usage{InputTokens: 12, OutputTokens: 3, TotalTokens: 15}
	if resp.Usage != wantUsage {
		t.Errorf("got usage %+v, want %+v", resp.Usage, wantUsage)
	}
}

func TestCompletePreservesOpaqueProviderDataAndRawToolCalls(t *testing.T) {
	rawToolCall := `{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"},"extra_content":{"google":{"thought_signature":"sig_tc"}}}`
	var secondRequestBody []byte
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_, _ = w.Write([]byte(`{
				"choices": [{
					"finish_reason": "tool_calls",
					"message": {
						"role": "assistant",
						"content": null,
						"tool_calls": [` + rawToolCall + `],
						"extra_content": {"google": {"thought_signature": "sig_msg"}}
					}
				}]
			}`))
			return
		}

		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode second request: %v", err)
		}
		secondRequestBody = body
		_, _ = w.Write([]byte(`{
			"choices": [{"finish_reason": "stop", "message": {"role": "assistant", "content": "done"}}]
		}`))
	}))
	defer server.Close()

	client := mustNewClient(t, server.URL, "secret-key", "gemini-3.6-flash")
	firstResp, err := client.Complete(context.Background(), nand.CompletionRequest{
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "read a.go"}},
	})
	if err != nil {
		t.Fatalf("first Complete error: %v", err)
	}
	if len(firstResp.Message.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(firstResp.Message.ToolCalls))
	}
	if firstResp.Message.ToolCalls[0].Name != "read" {
		t.Errorf("got tool name %q, want %q", firstResp.Message.ToolCalls[0].Name, "read")
	}

	_, err = client.Complete(context.Background(), nand.CompletionRequest{
		Messages: []nand.Message{
			{Role: nand.RoleUser, Content: "read a.go"},
			firstResp.Message,
			{Role: nand.RoleTool, ToolCallID: "call_1", Content: "package main"},
		},
	})
	if err != nil {
		t.Fatalf("second Complete error: %v", err)
	}

	sent := string(secondRequestBody)
	if !strings.Contains(sent, `"thought_signature":"sig_msg"`) {
		t.Errorf("message thought_signature was not preserved: %s", sent)
	}
	if !strings.Contains(sent, `"thought_signature":"sig_tc"`) {
		t.Errorf("tool_call thought_signature was not preserved: %s", sent)
	}
}

func TestCompleteRetriesOn429And503WithBackoff(t *testing.T) {
	statuses := []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusOK}
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := statuses[attempts]
		attempts++
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":"temporary"}`))
	}))
	defer server.Close()

	var sleeps []time.Duration
	client := mustNewClient(t, server.URL, "secret-key", "gemini-3.6-flash")
	client.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	resp, err := client.Complete(context.Background(), nand.CompletionRequest{
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Errorf("got %q, want %q", resp.Message.Content, "ok")
	}
	wantSleeps := []time.Duration{2 * time.Second, 4 * time.Second}
	if !reflect.DeepEqual(sleeps, wantSleeps) {
		t.Errorf("got sleeps %v, want %v", sleeps, wantSleeps)
	}
}

func TestCompleteExhaustsRetriesAndDoesNotLeakKey(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`unavailable`))
	}))
	defer server.Close()

	var sleeps []time.Duration
	client := mustNewClient(t, server.URL, "super-secret-api-key", "gemini-3.6-flash")
	client.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	_, err := client.Complete(context.Background(), nand.CompletionRequest{
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if attempts != 5 {
		t.Errorf("got %d attempts, want 5", attempts)
	}
	if !reflect.DeepEqual(sleeps, defaultBackoff) {
		t.Errorf("got sleeps %v, want %v", sleeps, defaultBackoff)
	}
	if strings.Contains(err.Error(), "super-secret-api-key") {
		t.Errorf("error leaked API key: %v", err)
	}
}

func TestCompleteDoesNotRetryNonRetryableStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad request`))
	}))
	defer server.Close()

	client := mustNewClient(t, server.URL, "secret-key", "gemini-3.6-flash")
	client.sleep = func(_ context.Context, _ time.Duration) error {
		t.Fatal("sleep should not be called for 400")
		return nil
	}

	_, err := client.Complete(context.Background(), nand.CompletionRequest{
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "hi"}},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("got err %v, want 400 APIError", err)
	}
	if attempts != 1 {
		t.Errorf("got %d attempts, want 1", attempts)
	}
}

func TestCompleteAbortsRetryOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := mustNewClient(t, server.URL, "secret-key", "gemini-3.6-flash")
	client.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := client.Complete(ctx, nand.CompletionRequest{
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "hi"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err %v, want context.Canceled", err)
	}
}

func TestCompleteSerializesToolDefinitions(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	client := mustNewClient(t, server.URL, "secret-key", "gemini-3.6-flash")
	_, err := client.Complete(context.Background(), nand.CompletionRequest{
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "hi"}},
		Tools: []nand.ToolDefinition{
			{
				Name:        "read",
				Description: "Read a file",
				Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if !strings.Contains(capturedBody, `"tools":[{"type":"function","function":{"name":"read"`) {
		t.Errorf("tools not serialized as expected: %s", capturedBody)
	}

	_, err = client.Complete(context.Background(), nand.CompletionRequest{
		Messages: []nand.Message{{Role: nand.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete without tools error: %v", err)
	}
	if strings.Contains(capturedBody, `"tools"`) {
		t.Errorf("empty tools should be omitted: %s", capturedBody)
	}
}

func mustNewClient(t *testing.T, baseURL, apiKey, model string) *Client {
	t.Helper()
	client, err := New(Config{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return client
}
