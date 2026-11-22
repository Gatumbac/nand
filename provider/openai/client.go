package openai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Gatumbac/nand"
)

const (
	defaultTimeout      = 60 * time.Second
	maxErrorBodyBytes   = 2048
	chatCompletionsPath = "/chat/completions"
)

type Config struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("provider request failed with status %d: %s", e.StatusCode, e.Body)
}

type Client struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
	backoff    []time.Duration
	sleep      func(context.Context, time.Duration) error
}

func New(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("openai provider: base URL is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("openai provider: API key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("openai provider: model is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		endpoint:   baseURL + chatCompletionsPath,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		httpClient: httpClient,
		backoff:    defaultBackoff,
		sleep:      sleepWithContext,
	}, nil
}

func (c *Client) Complete(ctx context.Context, req nand.CompletionRequest) (nand.CompletionResponse, error) {
	payload, err := encodeRequest(c.model, req)
	if err != nil {
		return nand.CompletionResponse{}, err
	}

	body, err := c.doWithRetry(ctx, payload)
	if err != nil {
		return nand.CompletionResponse{}, err
	}

	return decodeResponse(body)
}

func (c *Client) doWithRetry(ctx context.Context, payload []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= len(c.backoff); attempt++ {
		body, err := c.sendOnce(ctx, payload)
		if err == nil {
			return body, nil
		}
		lastErr = err

		var apiErr *APIError
		if !errors.As(err, &apiErr) || !isRetryableStatus(apiErr.StatusCode) {
			return nil, err
		}
		if attempt == len(c.backoff) {
			break
		}
		if err := c.sleep(ctx, c.backoff[attempt]); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) sendOnce(ctx context.Context, payload []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return nil, newAPIError(httpResp.StatusCode, body)
	}
	return body, nil
}

func newAPIError(statusCode int, body []byte) *APIError {
	msg := strings.TrimSpace(string(body))
	if len(msg) > maxErrorBodyBytes {
		msg = msg[:maxErrorBodyBytes] + "..."
	}
	return &APIError{StatusCode: statusCode, Body: msg}
}
