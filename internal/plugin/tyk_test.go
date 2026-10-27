package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/config"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/needlepath"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/policy"
)

func testGatewayContext() plugin_sdk.Context {
	return plugin_sdk.Context{Context: context.Background()}
}

func TestGatewayPluginExposesValidMetadata(t *testing.T) {
	p := NewGatewayPlugin()

	manifest, err := p.GetManifest()
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("manifest is invalid JSON: %v", err)
	}
	if decoded["id"] != "com.nextmoca.runtime-context-defense" {
		t.Fatalf("manifest id = %v", decoded["id"])
	}

	schema, err := p.GetConfigSchema()
	if err != nil {
		t.Fatalf("GetConfigSchema() error = %v", err)
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is invalid JSON: %v", err)
	}
}

func TestPackagedMetadataMatchesEmbeddedAssets(t *testing.T) {
	checks := []struct {
		packaged string
		embedded []byte
	}{
		{packaged: "../../manifest.json", embedded: manifestFile},
		{packaged: "../../config.schema.json", embedded: configSchemaFile},
	}

	for _, check := range checks {
		packaged, err := os.ReadFile(check.packaged)
		if err != nil {
			t.Fatalf("read %s: %v", check.packaged, err)
		}
		if !bytes.Equal(packaged, check.embedded) {
			t.Fatalf("packaged metadata differs from embedded %s", check.packaged)
		}
	}
}

func TestGatewayPluginTransformsPostAuthRequest(t *testing.T) {
	p := NewGatewayPlugin()
	p.core = NewForTest(testConfig(config.ModeActive), &fakeSelector{response: needlepath.Response{
		Outcome: needlepath.OutcomeEngaged,
		Selected: []needlepath.SelectedRecord{{
			RecordID: "tyk:0",
			Excerpt:  "Reservation confirmation code is H7K2.",
		}},
	}}, nil)

	response, err := p.HandlePostAuth(testGatewayContext(), &pb.EnrichedRequest{Request: &pb.PluginRequest{
		Method: "POST",
		Body:   []byte(providerRequest),
	}})
	if err != nil {
		t.Fatalf("HandlePostAuth() error = %v", err)
	}
	if !response.Modified || response.Block {
		t.Fatalf("response = %#v, want modified non-blocking response", response)
	}
	if string(response.Body) == providerRequest {
		t.Fatal("plugin did not replace request body")
	}
}

func TestGatewayPluginBlocksEnforcedUnsafeRequest(t *testing.T) {
	p := NewGatewayPlugin()
	p.core = NewForTest(testConfig(config.ModeEnforce), &fakeSelector{response: needlepath.Response{
		Outcome: needlepath.OutcomeBlocked,
	}}, nil)

	response, err := p.HandlePostAuth(testGatewayContext(), &pb.EnrichedRequest{Request: &pb.PluginRequest{
		Method: "POST",
		Body:   []byte(providerRequest),
	}})
	if err != nil {
		t.Fatalf("HandlePostAuth() error = %v", err)
	}
	if !response.Block || response.StatusCode != 403 {
		t.Fatalf("response = %#v, want 403 blocking response", response)
	}
}

func TestGatewayPluginInitializationFailureBlocksAtTheRequestBoundary(t *testing.T) {
	p := NewGatewayPlugin()
	if err := p.Initialize(testGatewayContext(), map[string]string{
		"api_key_env":     "MISSING_NEXTMOCA_API_KEY",
		"operating_point": "np-2026-10-r8",
	}); err != nil {
		t.Fatalf("Initialize() error = %v, want the plugin to stay loaded and fail closed", err)
	}

	response, err := p.HandlePostAuth(testGatewayContext(), &pb.EnrichedRequest{Request: &pb.PluginRequest{
		Method: "POST",
		Body:   []byte(providerRequest),
	}})
	if err != nil {
		t.Fatalf("HandlePostAuth() error = %v", err)
	}
	if !response.Block || response.Modified || response.StatusCode != 503 {
		t.Fatalf("response = %#v, want an unavailable 503 block", response)
	}
}

func TestBlockResponseReportsTheCauseOfTheBlock(t *testing.T) {
	cases := []struct {
		cause      policy.BlockCause
		wantStatus int
		wantType   string
	}{
		{cause: policy.CauseVerdict, wantStatus: 403, wantType: "context_security_blocked"},
		{cause: policy.CauseUnavailable, wantStatus: 503, wantType: "context_security_unavailable"},
		{cause: policy.CauseUnsupported, wantStatus: 422, wantType: "context_security_unsupported_request"},
		{cause: "", wantStatus: 403, wantType: "context_security_blocked"},
	}
	for _, tc := range cases {
		t.Run(string(tc.cause), func(t *testing.T) {
			response := responseForDecision(policy.Decision{Kind: policy.Block, Cause: tc.cause})
			if !response.Block || response.Modified || int(response.StatusCode) != tc.wantStatus {
				t.Fatalf("response = %#v, want blocking status %d", response, tc.wantStatus)
			}
			var body struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Type != tc.wantType {
				t.Fatalf("error type = %q, want %q", body.Error.Type, tc.wantType)
			}
		})
	}
}

func TestSinkAlwaysLogsTheFailureClassEvenWithTelemetryOff(t *testing.T) {
	logged := captureLog(t, func() {
		gatewayReceiptSink{enabled: false}.Record(Receipt{
			Reason:     "selection_error",
			ErrorClass: "quota",
			Outcome:    "should-not-appear",
		})
	})
	if !strings.Contains(logged, `class="quota"`) || !strings.Contains(logged, `reason="selection_error"`) {
		t.Fatalf("log = %q, want the failure class and reason", logged)
	}
	if strings.Contains(logged, "should-not-appear") || strings.Contains(logged, "tokens_before") {
		t.Fatalf("telemetry-off log carries receipt fields: %q", logged)
	}
}

func TestSinkLogsNothingWithTelemetryOffWhenNothingFailed(t *testing.T) {
	logged := captureLog(t, func() {
		gatewayReceiptSink{enabled: false}.Record(Receipt{Reason: "selected", Outcome: "engaged"})
	})
	if logged != "" {
		t.Fatalf("log = %q, want nothing", logged)
	}
}

func TestSinkQuotesServerControlledStrings(t *testing.T) {
	logged := captureLog(t, func() {
		gatewayReceiptSink{enabled: true}.Record(Receipt{
			Outcome:     "engaged tokens_saved=999999",
			SelectionID: "id\nforged-line selection_id=x",
		})
	})
	if strings.Count(logged, "\n") != 1 {
		t.Fatalf("log injection: %q", logged)
	}
	if !strings.Contains(logged, `outcome="engaged tokens_saved=999999"`) {
		t.Fatalf("outcome not quoted: %q", logged)
	}
}

func captureLog(t *testing.T, emit func()) string {
	t.Helper()
	var buffer bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	}()
	emit()
	return buffer.String()
}
