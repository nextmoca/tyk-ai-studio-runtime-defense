package needlepath

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/config"
)

func TestSelectSendsRecordsAndQuarantineMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/context/select" {
			t.Fatalf("path = %q, want /v1/context/select", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer np_live_test" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request = %v", err)
		}
		if got := body["security_mode"]; got != "quarantine" {
			t.Fatalf("security_mode = %#v, want quarantine", got)
		}
		if got := body["records"].([]any); len(got) != 1 {
			t.Fatalf("records length = %d, want 1", len(got))
		}
		requestID, ok := body["request_id"].(string)
		if !ok || requestID == "" {
			t.Fatalf("request_id = %#v, want generated request ID", body["request_id"])
		}
		budget := body["budget"].(map[string]any)
		if got := budget["operating_point"]; got != "np-2026-10-r7" {
			t.Fatalf("budget.operating_point = %#v", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"request_id":%q,
			"rendered_context":"clean evidence",
			"tokens_before":100,
			"tokens_after":42,
			"tokens_saved":58,
			"fallback_used":false,
			"records_available":1,
			"records_selected":1,
			"outcome":"engaged",
			"selected":[{"record_id":"tyk:2","excerpt":"Reservation H7K2"}]
		}`, requestID)))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL, config.ModeQuarantine), server.Client())
	response, err := client.Select(context.Background(), Request{
		Records: []Record{{
			ID:         "tyk:2",
			Kind:       "tool_result",
			Source:     "tyk",
			SourceKind: "tool",
			Text:       "Reservation H7K2",
		}},
		TaskPrompt: "Find the reservation confirmation code.",
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if response.Outcome != OutcomeEngaged {
		t.Fatalf("Outcome = %q, want %q", response.Outcome, OutcomeEngaged)
	}
	if response.RenderedContext != "clean evidence" {
		t.Fatalf("RenderedContext = %q", response.RenderedContext)
	}
	if len(response.Selected) != 1 || response.Selected[0].RecordID != "tyk:2" || response.Selected[0].Excerpt != "Reservation H7K2" {
		t.Fatalf("Selected = %#v, want mapped selection", response.Selected)
	}
}

func TestSelectRejectsResponseForAnotherRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"request_id":"tyk-another-request",
			"rendered_context":"clean evidence",
			"tokens_before":100,
			"tokens_after":42,
			"tokens_saved":58,
			"fallback_used":false,
			"outcome":"engaged",
			"selected":[]
		}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL, config.ModeEnforce), server.Client())
	_, err := client.Select(context.Background(), Request{TaskPrompt: "Find the answer."})
	if got := ClassOf(err); got != ClassContract {
		t.Fatalf("ClassOf(err) = %q, want %q", got, ClassContract)
	}
}

func TestSelectRejectsResponseWithoutRequiredSafetyFields(t *testing.T) {
	for _, omitted := range []string{"outcome", "fallback_used"} {
		t.Run(omitted, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatalf("decode request = %v", err)
				}
				response := map[string]any{
					"request_id":       request["request_id"],
					"rendered_context": "clean evidence",
					"tokens_before":    100,
					"tokens_after":     42,
					"tokens_saved":     58,
					"fallback_used":    false,
					"outcome":          "engaged",
					"selected":         []any{},
				}
				delete(response, omitted)
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Fatalf("encode response = %v", err)
				}
			}))
			defer server.Close()

			client := NewClient(testConfig(server.URL, config.ModeEnforce), server.Client())
			_, err := client.Select(context.Background(), Request{TaskPrompt: "Find the answer."})
			if got := ClassOf(err); got != ClassContract {
				t.Fatalf("ClassOf(err) = %q, want %q", got, ClassContract)
			}
		})
	}
}

func TestSelectRejectsIncompleteServerResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rendered_context":"x","tokens_before":1,"tokens_after":1,"tokens_saved":0}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL, config.ModeActive), server.Client())
	_, err := client.Select(context.Background(), Request{TaskPrompt: "Find the answer."})
	if err == nil {
		t.Fatal("Select() error = nil, want response-contract error")
	}
	if got := ClassOf(err); got != ClassContract {
		t.Fatalf("ClassOf(err) = %q, want %q", got, ClassContract)
	}
}

func TestSelectClassifiesHTTPStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorClass
	}{
		{status: http.StatusUnauthorized, want: ClassAuth},
		{status: http.StatusForbidden, want: ClassAuth},
		{status: http.StatusPaymentRequired, want: ClassQuota},
		{status: http.StatusTooManyRequests, want: ClassRateLimit},
		{status: http.StatusRequestEntityTooLarge, want: ClassOversize},
		{status: http.StatusBadRequest, want: ClassContract},
		{status: http.StatusUnprocessableEntity, want: ClassContract},
		{status: http.StatusInternalServerError, want: ClassServer},
		{status: http.StatusBadGateway, want: ClassServer},
		{status: http.StatusServiceUnavailable, want: ClassServer},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			client := NewClient(testConfig(server.URL, config.ModeEnforce), server.Client())
			_, err := client.Select(context.Background(), Request{TaskPrompt: "Find the answer."})
			if err == nil {
				t.Fatal("Select() error = nil, want a classified error")
			}
			if got := ClassOf(err); got != tc.want {
				t.Fatalf("ClassOf(err) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSelectClassifiesTransportFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "deadline", err: &url.Error{Op: "Post", URL: "https://example.test", Err: context.DeadlineExceeded}, want: ClassTimeout},
		{name: "connection refused", err: &url.Error{Op: "Post", URL: "https://example.test", Err: errors.New("connection refused")}, want: ClassTransport},
		{name: "cancelled", err: &url.Error{Op: "Post", URL: "https://example.test", Err: context.Canceled}, want: ClassCanceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(testConfig("https://example.test", config.ModeEnforce), failingDoer{err: tc.err})
			_, err := client.Select(context.Background(), Request{TaskPrompt: "Find the answer."})
			if got := ClassOf(err); got != tc.want {
				t.Fatalf("ClassOf(err) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSelectTimesOutAgainstASlowServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only notices a closed connection after the request body
		// has been read, so drain it before waiting for the client to give up.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL, config.ModeEnforce)
	cfg.Timeout = 30 * time.Millisecond
	client := NewClient(cfg, server.Client())
	_, err := client.Select(context.Background(), Request{TaskPrompt: "Find the answer."})
	if got := ClassOf(err); got != ClassTimeout {
		t.Fatalf("ClassOf(err) = %q, want %q", got, ClassTimeout)
	}
}

func TestSelectRejectsOversizeRequestWithoutCallingTheAPI(t *testing.T) {
	doer := &countingDoer{}
	client := NewClient(testConfig("https://example.test", config.ModeEnforce), doer)
	_, err := client.Select(context.Background(), Request{
		TaskPrompt: "Find the answer.",
		Records:    []Record{{ID: "tyk:0", Text: strings.Repeat("x", maxRequestBytes)}},
	})
	if got := ClassOf(err); got != ClassOversize {
		t.Fatalf("ClassOf(err) = %q, want %q", got, ClassOversize)
	}
	if doer.calls != 0 {
		t.Fatalf("API was called %d times for an oversize request", doer.calls)
	}
}

func TestSelectErrorTextNeverCarriesTheAPIKeyOrRequestContent(t *testing.T) {
	const content = "confidential reservation H7K2"
	client := NewClient(testConfig("https://example.test", config.ModeEnforce), failingDoer{err: errors.New("connection refused")})
	_, err := client.Select(context.Background(), Request{
		TaskPrompt: "Find the answer.",
		Records:    []Record{{ID: "tyk:0", Text: content}},
	})
	if err == nil {
		t.Fatal("Select() error = nil")
	}
	for _, secret := range []string{"np_live_test", content} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error text %q contains %q", err.Error(), secret)
		}
	}
}

func TestSelectOpensTheCircuitAfterRepeatedOutagesAndSkipsTheAPI(t *testing.T) {
	doer := &countingDoer{err: &url.Error{Op: "Post", URL: "https://example.test", Err: context.DeadlineExceeded}}
	cfg := testConfig("https://example.test", config.ModeEnforce)
	cfg.BreakerFailures = 2
	cfg.BreakerCooldown = time.Hour
	client := NewClient(cfg, doer)
	request := Request{TaskPrompt: "Find the answer."}

	for call := 0; call < 2; call++ {
		if got := ClassOf(selectError(t, client, request)); got != ClassTimeout {
			t.Fatalf("call %d class = %q, want %q", call, got, ClassTimeout)
		}
	}
	if got := ClassOf(selectError(t, client, request)); got != ClassCircuitOpen {
		t.Fatalf("class with open circuit = %q, want %q", got, ClassCircuitOpen)
	}
	if doer.calls != 2 {
		t.Fatalf("API calls = %d, want 2 (no call while the circuit is open)", doer.calls)
	}
}

func selectError(t *testing.T, client *Client, request Request) error {
	t.Helper()
	_, err := client.Select(context.Background(), request)
	if err == nil {
		t.Fatal("Select() error = nil, want an error")
	}
	return err
}

type failingDoer struct {
	err error
}

func (d failingDoer) Do(*http.Request) (*http.Response, error) {
	return nil, d.err
}

type countingDoer struct {
	calls int
	err   error
}

func (d *countingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return nil, d.err
}

func testConfig(endpoint string, mode config.Mode) config.Config {
	return config.Config{
		Endpoint:         endpoint,
		APIKey:           "np_live_test",
		Mode:             mode,
		OperatingPoint:   "np-2026-10-r7",
		MaxContextTokens: 8192,
		CapacityTokens:   128000,
	}
}

func TestSelectDoesNotFollowRedirects(t *testing.T) {
	var targetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
	}))
	defer target.Close()
	var sourceHits int
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceHits++
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := NewClient(testConfig(source.URL, config.ModeEnforce), NewHTTPClient())
	_, err := client.Select(context.Background(), Request{TaskPrompt: "Find the answer."})
	if got := ClassOf(err); got != ClassContract {
		t.Fatalf("ClassOf(err) = %q, want %q", got, ClassContract)
	}
	if sourceHits != 1 || targetHits != 0 {
		t.Fatalf("hits = (source %d, target %d), want (1, 0): a redirect must not replay the request", sourceHits, targetHits)
	}
}
