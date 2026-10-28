package plugin

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/config"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/needlepath"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/policy"
)

const (
	PluginName    = "nextmoca-runtime-context-defense"
	PluginVersion = "0.1.3"
)

//go:embed assets/manifest.json
var manifestFile []byte

//go:embed assets/config.schema.json
var configSchemaFile []byte

type GatewayPlugin struct {
	plugin_sdk.BasePlugin
	core                       *Plugin
	passThroughOnInvalidConfig bool
}

func NewGatewayPlugin() *GatewayPlugin {
	return &GatewayPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(
			PluginName,
			PluginVersion,
			"Next Moca Runtime Defense for AI Agents on Tyk AI Studio",
		),
	}
}

func (p *GatewayPlugin) Initialize(_ plugin_sdk.Context, values map[string]string) error {
	cfg, err := config.Parse(values, os.Getenv)
	if err != nil {
		// Tyk can probe plugin metadata before applying its configured values.
		// Keep the process available for the later configured initialization, but
		// never allow a request through while the plugin has no valid policy.
		p.core = nil
		p.passThroughOnInvalidConfig = config.InvalidConfigurationFailsOpen(values)
		if p.passThroughOnInvalidConfig {
			log.Printf("%s initialization unavailable: %v; shadow/active requests will pass through until configuration is valid", PluginName, err)
		} else {
			log.Printf("%s initialization unavailable: %v; screening-mode requests will return 503 until configuration is valid", PluginName, err)
		}
		return nil
	}
	p.core = NewForTest(cfg, needlepath.NewClient(cfg, needlepath.NewHTTPClient()), gatewayReceiptSink{enabled: cfg.TelemetryEnabled})
	p.passThroughOnInvalidConfig = false
	log.Printf(
		"%s initialized mode=%s operating_point=%q autochunk=%t verify_literal_excerpts=%t on_selection_error=%s unsupported_shape=%s timeout=%s",
		PluginName,
		cfg.Mode,
		cfg.OperatingPoint,
		cfg.Autochunk,
		cfg.VerifyLiteralExcerpts,
		cfg.OnSelectionError,
		cfg.UnsupportedShape,
		cfg.Timeout,
	)
	return nil
}

func (p *GatewayPlugin) GetManifest() ([]byte, error) {
	return manifestFile, nil
}

func (p *GatewayPlugin) GetConfigSchema() ([]byte, error) {
	return configSchemaFile, nil
}

func (p *GatewayPlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (*pb.PluginResponse, error) {
	if req == nil || req.Request == nil {
		return &pb.PluginResponse{Modified: false}, nil
	}
	if p.core == nil {
		if p.passThroughOnInvalidConfig {
			return &pb.PluginResponse{Modified: false}, nil
		}
		return blockResponse(policy.CauseUnavailable), nil
	}

	requestContext := context.Background()
	if ctx.Context != nil {
		requestContext = ctx.Context
	}
	result := p.core.Process(requestContext, req.Request.Method, req.Request.Body)
	return responseForDecision(result.Decision), nil
}

func responseForDecision(decision policy.Decision) *pb.PluginResponse {
	switch decision.Kind {
	case policy.Transform:
		return &pb.PluginResponse{
			Modified: true,
			Headers:  map[string]string{"Content-Type": "application/json"},
			Body:     decision.Body,
		}
	case policy.Block:
		return blockResponse(decision.Cause)
	default:
		return &pb.PluginResponse{Modified: false}
	}
}

// blockResponse answers a Block decision. A security verdict is a 403. A
// selection failure or an ungovernable request shape, refused because the
// operator chose block over pass, is not a verdict and is reported as such.
func blockResponse(cause policy.BlockCause) *pb.PluginResponse {
	response := &pb.PluginResponse{
		Block:   true,
		Headers: map[string]string{"Content-Type": "application/json"},
	}
	errorType := "context_security_blocked"
	message := "Request blocked by Next Moca context security."
	switch cause {
	case policy.CauseUnavailable:
		response.StatusCode = http.StatusServiceUnavailable
		errorType = "context_security_unavailable"
		message = "Next Moca context security could not evaluate this request."
	case policy.CauseUnsupported:
		response.StatusCode = http.StatusUnprocessableEntity
		errorType = "context_security_unsupported_request"
		message = "Next Moca context security does not support this request shape."
	default:
		response.StatusCode = http.StatusForbidden
	}
	response.Body, _ = json.Marshal(map[string]any{
		"error": map[string]string{
			"type":    errorType,
			"message": message,
		},
	})
	return response
}

type gatewayReceiptSink struct {
	enabled bool
}

// Record writes content-free log lines. A failed selection is always logged
// with its failure class, because it means a request was not governed as
// configured; the decision receipt itself is logged only when telemetry is
// enabled. Server-supplied strings are quoted.
func (s gatewayReceiptSink) Record(receipt Receipt) {
	if !s.enabled {
		if receipt.ErrorClass != "" {
			log.Printf("%s selection_failure class=%q reason=%q", PluginName, receipt.ErrorClass, receipt.Reason)
		}
		return
	}
	log.Printf(
		"%s receipt outcome=%q reason=%q error_class=%q tokens_before=%d tokens_after=%d tokens_saved=%d engine_latency_ms=%.2f selection_id=%q",
		PluginName,
		receipt.Outcome,
		receipt.Reason,
		receipt.ErrorClass,
		receipt.TokensBefore,
		receipt.TokensAfter,
		receipt.TokensSaved,
		receipt.EngineLatencyMS,
		receipt.SelectionID,
	)
}

var (
	_ plugin_sdk.PostAuthHandler  = (*GatewayPlugin)(nil)
	_ plugin_sdk.ConfigProvider   = (*GatewayPlugin)(nil)
	_ plugin_sdk.ManifestProvider = (*GatewayPlugin)(nil)
)
