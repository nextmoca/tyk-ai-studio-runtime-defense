package needlepath

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/config"
)

const (
	selectPath       = "/v1/context/select"
	maxRequestBytes  = 6 * 1024 * 1024
	maxResponseBytes = 6 * 1024 * 1024

	OutcomeEngaged           = "engaged"
	OutcomeSelected          = "selected"
	OutcomeSanitizedFallback = "sanitized_fallback"
	OutcomeBlocked           = "blocked"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	config  config.Config
	http    HTTPDoer
	breaker *breaker
}

type Record struct {
	ID         string
	Kind       string
	Source     string
	SourceKind string
	Text       string
}

type Request struct {
	Records    []Record
	TaskPrompt string
}

type Response struct {
	RequestID       string
	SelectionID     string
	Outcome         string
	RenderedContext string
	TokensBefore    int
	TokensAfter     int
	TokensSaved     int
	EngineLatencyMS float64
	FallbackUsed    bool
	Selected        []SelectedRecord
}

type SelectedRecord struct {
	RecordID string
	Excerpt  string
}

// NewHTTPClient returns the HTTP client the plugin uses for the hosted API. It
// never follows redirects: the plugin makes one attempt per request, and
// following a 307 or 308 would send the request body again to another URL. A
// redirect is returned as a response and classified as a contract error.
func NewHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func NewClient(cfg config.Config, httpClient HTTPDoer) *Client {
	if httpClient == nil {
		httpClient = NewHTTPClient()
	}
	return &Client{
		config:  cfg,
		http:    httpClient,
		breaker: newBreaker(cfg.BreakerFailures, cfg.BreakerCooldown, nil),
	}
}

func (c *Client) Select(ctx context.Context, request Request) (Response, error) {
	if strings.TrimSpace(request.TaskPrompt) == "" {
		return Response{}, newError(ClassRequest, fmt.Errorf("task prompt is required"))
	}

	requestID := newRequestID()
	body, err := json.Marshal(c.wireRequest(request, requestID))
	if err != nil {
		return Response{}, newError(ClassRequest, fmt.Errorf("encode Needlepath request: %w", err))
	}
	if len(body) > maxRequestBytes {
		return Response{}, newError(ClassOversize, fmt.Errorf("Needlepath request exceeds %d bytes", maxRequestBytes))
	}

	admission, allowed := c.breaker.allow()
	if !allowed {
		return Response{}, newError(ClassCircuitOpen, fmt.Errorf("Needlepath circuit breaker is open"))
	}
	response, err := c.send(ctx, body, requestID)
	c.breaker.report(admission, err)
	return response, err
}

func (c *Client) send(ctx context.Context, body []byte, requestID string) (Response, error) {
	requestContext := ctx
	if c.config.Timeout > 0 {
		var cancel context.CancelFunc
		requestContext, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}

	httpRequest, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		strings.TrimRight(c.config.Endpoint, "/")+selectPath,
		bytes.NewReader(body),
	)
	if err != nil {
		return Response{}, newError(ClassRequest, fmt.Errorf("create Needlepath request: %w", err))
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	httpResponse, err := c.http.Do(httpRequest)
	if err != nil {
		return Response{}, newError(classForTransport(err), fmt.Errorf("call Needlepath: %w", err))
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return Response{}, newError(
			classForStatus(httpResponse.StatusCode),
			fmt.Errorf("Needlepath returned HTTP %d", httpResponse.StatusCode),
		)
	}

	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes+1))
	if err != nil {
		return Response{}, newError(classForTransport(err), fmt.Errorf("read Needlepath response: %w", err))
	}
	if len(responseBody) > maxResponseBytes {
		return Response{}, newError(ClassOversize, fmt.Errorf("Needlepath response exceeds %d bytes", maxResponseBytes))
	}

	response, err := decodeResponse(responseBody, requestID)
	if err != nil {
		return Response{}, newError(ClassContract, err)
	}
	response.SelectionID = httpResponse.Header.Get("X-Needlepath-Selection-Id")
	return response, nil
}

func (c *Client) wireRequest(request Request, requestID string) map[string]any {
	records := make([]map[string]any, 0, len(request.Records))
	for _, record := range request.Records {
		records = append(records, map[string]any{
			"id":          record.ID,
			"kind":        record.Kind,
			"source":      record.Source,
			"source_kind": record.SourceKind,
			"text":        record.Text,
		})
	}
	return map[string]any{
		"request_id": requestID,
		"records":    records,
		"task": map[string]any{
			"prompt": request.TaskPrompt,
		},
		"budget": map[string]any{
			"max_context_tokens":  c.config.MaxContextTokens,
			"capacity_cap_tokens": c.config.CapacityTokens,
			"operating_point":     c.config.OperatingPoint,
		},
		"render":            true,
		"render_format":     "plain",
		"return_per_record": true,
		"security_mode":     securityMode(c.config.Mode),
	}
}

func securityMode(mode config.Mode) string {
	switch mode {
	case config.ModeShadow:
		return "audit"
	case config.ModeQuarantine:
		return "quarantine"
	case config.ModeEnforce:
		return "enforce"
	default:
		return "off"
	}
}

func decodeResponse(raw []byte, expectedRequestID string) (Response, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return Response{}, fmt.Errorf("decode Needlepath response: %w", err)
	}

	requestID, ok := body["request_id"].(string)
	if !ok || strings.TrimSpace(requestID) == "" {
		return Response{}, fmt.Errorf("invalid Needlepath response: request_id is required")
	}
	if requestID != expectedRequestID {
		return Response{}, fmt.Errorf("invalid Needlepath response: request_id does not match request")
	}
	renderedContext, ok := body["rendered_context"].(string)
	if !ok {
		return Response{}, fmt.Errorf("invalid Needlepath response: rendered_context is required")
	}
	tokensBefore, err := requiredInt(body, "tokens_before")
	if err != nil {
		return Response{}, err
	}
	tokensAfter, err := requiredInt(body, "tokens_after")
	if err != nil {
		return Response{}, err
	}
	tokensSaved, err := requiredInt(body, "tokens_saved")
	if err != nil {
		return Response{}, err
	}
	outcome, ok := body["outcome"].(string)
	if !ok || strings.TrimSpace(outcome) == "" {
		return Response{}, fmt.Errorf("invalid Needlepath response: outcome is required")
	}
	fallbackUsed, ok := body["fallback_used"].(bool)
	if !ok {
		return Response{}, fmt.Errorf("invalid Needlepath response: fallback_used is required")
	}

	response := Response{
		RequestID:       requestID,
		RenderedContext: renderedContext,
		TokensBefore:    tokensBefore,
		TokensAfter:     tokensAfter,
		TokensSaved:     tokensSaved,
		Outcome:         outcome,
		FallbackUsed:    fallbackUsed,
	}
	if latency, ok := number(body["engine_latency_ms"]); ok {
		response.EngineLatencyMS = latency
	}
	selected, err := decodeSelected(body["selected"])
	if err != nil {
		return Response{}, err
	}
	response.Selected = selected
	return response, nil
}

func decodeSelected(value any) ([]SelectedRecord, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid Needlepath response: selected must be an array")
	}
	selected := make([]SelectedRecord, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid Needlepath response: selected item must be an object")
		}
		recordID, ok := record["record_id"].(string)
		if !ok || recordID == "" {
			return nil, fmt.Errorf("invalid Needlepath response: selected record_id is required")
		}
		excerpt, ok := record["excerpt"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid Needlepath response: selected excerpt is required")
		}
		selected = append(selected, SelectedRecord{RecordID: recordID, Excerpt: excerpt})
	}
	return selected, nil
}

func requiredInt(body map[string]any, field string) (int, error) {
	value, ok := number(body[field])
	if !ok {
		return 0, fmt.Errorf("invalid Needlepath response: %s is required", field)
	}
	return int(value), nil
}

func number(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok
}

func newRequestID() string {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err == nil {
		return "tyk-" + hex.EncodeToString(entropy[:])
	}
	return fmt.Sprintf("tyk-%d", time.Now().UnixNano())
}
