package plugin

import (
	"context"

	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/config"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/needlepath"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/openai"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/policy"
)

type Selector interface {
	Select(context.Context, needlepath.Request) (needlepath.Response, error)
}

type Receipt struct {
	Outcome         string
	Reason          string
	ErrorClass      string
	TokensBefore    int
	TokensAfter     int
	TokensSaved     int
	EngineLatencyMS float64
	SelectionID     string
}

type ReceiptSink interface {
	Record(Receipt)
}

type ProcessResult struct {
	Decision policy.Decision
}

type Plugin struct {
	config   config.Config
	selector Selector
	sink     ReceiptSink
}

func NewForTest(cfg config.Config, selector Selector, sink ReceiptSink) *Plugin {
	return &Plugin{config: cfg, selector: selector, sink: sink}
}

func (p *Plugin) Process(ctx context.Context, method string, raw []byte) ProcessResult {
	if method != "POST" || p.selector == nil {
		return p.passThrough(raw, "unsupported_request")
	}
	request, support, err := openai.Parse(raw)
	if err != nil {
		return p.passThrough(raw, "request_parse_error")
	}
	switch support {
	case openai.Supported:
		if request.HasFunctionRole() && p.blocksUnsupportedShape() {
			return p.blockUnsupportedShape("unsupported_shape_function_role")
		}
	case openai.NotChat:
		return p.passThrough(raw, "unsupported_provider_shape")
	default:
		if p.blocksUnsupportedShape() {
			return p.blockUnsupportedShape("unsupported_shape_" + string(support))
		}
		return p.passThrough(raw, "unsupported_provider_shape")
	}
	records := request.Records()
	if len(records) == 0 {
		return p.passThrough(raw, "no_eligible_context")
	}

	selectionRequest := needlepath.Request{
		Records:    make([]needlepath.Record, 0, len(records)),
		TaskPrompt: request.TaskPrompt(),
	}
	for _, record := range records {
		selectionRequest.Records = append(selectionRequest.Records, needlepath.Record{
			ID:         record.ID,
			Kind:       record.Kind,
			Source:     record.Source,
			SourceKind: record.SourceKind,
			Text:       record.Text,
		})
	}

	response, selectErr := p.selector.Select(ctx, selectionRequest)
	decision := policy.Decide(
		policy.Settings{
			Mode:                  p.config.Mode,
			OnSelectionError:      p.config.OnSelectionError,
			VerifyLiteralExcerpts: p.config.VerifyLiteralExcerpts,
		},
		raw,
		request,
		response,
		selectErr,
	)
	p.record(decision.Receipt)
	return ProcessResult{Decision: decision}
}

func (p *Plugin) passThrough(raw []byte, reason string) ProcessResult {
	decision := policy.Decision{
		Kind: policy.PassThrough,
		Body: raw,
		Receipt: policy.Receipt{
			Reason: reason,
		},
	}
	p.record(decision.Receipt)
	return ProcessResult{Decision: decision}
}

// blocksUnsupportedShape reports whether a request the plugin cannot govern is
// refused rather than passed through unchanged.
func (p *Plugin) blocksUnsupportedShape() bool {
	return p.config.UnsupportedShape == config.ActionBlock && p.config.Mode.Screens()
}

func (p *Plugin) blockUnsupportedShape(reason string) ProcessResult {
	decision := policy.Decision{
		Kind:    policy.Block,
		Cause:   policy.CauseUnsupported,
		Receipt: policy.Receipt{Reason: reason},
	}
	p.record(decision.Receipt)
	return ProcessResult{Decision: decision}
}

func (p *Plugin) record(receipt policy.Receipt) {
	if p.sink == nil {
		return
	}
	p.sink.Record(Receipt{
		Outcome:         receipt.Outcome,
		Reason:          receipt.Reason,
		ErrorClass:      receipt.ErrorClass,
		TokensBefore:    receipt.TokensBefore,
		TokensAfter:     receipt.TokensAfter,
		TokensSaved:     receipt.TokensSaved,
		EngineLatencyMS: receipt.EngineLatencyMS,
		SelectionID:     receipt.SelectionID,
	})
}
