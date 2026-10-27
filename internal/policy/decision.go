package policy

import (
	"fmt"
	"strings"

	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/config"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/needlepath"
)

type Kind string

const (
	PassThrough Kind = "pass_through"
	Transform   Kind = "transform"
	Block       Kind = "block"
)

// BlockCause says why a Block decision was made, so the gateway can answer
// with an accurate status.
type BlockCause string

const (
	// CauseVerdict is a security verdict or a sanitized outcome that cannot
	// be delivered without restoring raw content.
	CauseVerdict BlockCause = "verdict"
	// CauseUnavailable is a selection failure with on_selection_error=block.
	CauseUnavailable BlockCause = "unavailable"
	// CauseUnsupported is a request shape the plugin cannot govern, with
	// unsupported_shape=block.
	CauseUnsupported BlockCause = "unsupported"
)

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

type Decision struct {
	Kind    Kind
	Cause   BlockCause
	Body    []byte
	Receipt Receipt
}

// Settings are the configured delivery policies Decide applies.
type Settings struct {
	Mode             config.Mode
	OnSelectionError config.Action
}

// Rewriter is the part of a parsed request the policy needs: which records
// exist and how to rebuild the body from server-supplied content.
type Rewriter interface {
	CandidateCount() int
	HasCandidate(id string) bool
	RewriteSelected(excerpts map[string]string) ([]byte, error)
	RewriteSingleCandidate(rendered string) ([]byte, error)
}

func Decide(
	settings Settings,
	original []byte,
	request Rewriter,
	response needlepath.Response,
	selectErr error,
) Decision {
	mode := settings.Mode
	receipt := receiptFor(response)
	if selectErr != nil {
		receipt.Reason = "selection_error"
		receipt.ErrorClass = string(needlepath.ClassOf(selectErr))
		if settings.OnSelectionError == config.ActionBlock && mode.Screens() {
			receipt.Reason = "selection_error_blocked"
			return Decision{Kind: Block, Cause: CauseUnavailable, Receipt: receipt}
		}
		return passThrough(original, receipt)
	}
	if mode == config.ModeShadow {
		receipt.Reason = "shadow"
		return passThrough(original, receipt)
	}

	outcome := strings.ToLower(strings.TrimSpace(response.Outcome))
	switch outcome {
	case needlepath.OutcomeBlocked:
		if mode == config.ModeEnforce {
			receipt.Reason = "blocked_by_needlepath"
			return Decision{Kind: Block, Cause: CauseVerdict, Receipt: receipt}
		}
		receipt.Reason = "blocked_observed_not_enforced"
		return passThrough(original, receipt)
	case needlepath.OutcomeSanitizedFallback:
		return sanitizedFallback(mode, original, request, response, receipt)
	case needlepath.OutcomeEngaged, needlepath.OutcomeSelected:
		return selected(mode, original, request, response, receipt)
	case "":
		// Older hosted deployments may omit outcome. Treat the structural
		// selection shape as compatible only when no fallback directive exists.
		if !response.FallbackUsed && len(response.Selected) > 0 {
			return selected(mode, original, request, response, receipt)
		}
		receipt.Reason = "no_delivery_outcome"
		return passThrough(original, receipt)
	default:
		receipt.Reason = "unknown_delivery_outcome"
		return passThrough(original, receipt)
	}
}

func selected(
	mode config.Mode,
	original []byte,
	request Rewriter,
	response needlepath.Response,
	receipt Receipt,
) Decision {
	if response.FallbackUsed || len(response.Selected) == 0 {
		receipt.Reason = "selection_not_deliverable"
		return passThrough(original, receipt)
	}

	excerpts := make(map[string]string, len(response.Selected))
	for _, selected := range response.Selected {
		if request.HasCandidate(selected.RecordID) && strings.TrimSpace(selected.Excerpt) != "" {
			excerpts[selected.RecordID] = selected.Excerpt
		}
	}
	if len(excerpts) == 0 {
		receipt.Reason = "selection_has_no_matching_candidate"
		return passThrough(original, receipt)
	}
	body, err := request.RewriteSelected(excerpts)
	if err != nil {
		receipt.Reason = "selection_rewrite_failed"
		return passThrough(original, receipt)
	}
	receipt.Reason = "selected"
	return Decision{Kind: Transform, Body: body, Receipt: receipt}
}

func sanitizedFallback(
	mode config.Mode,
	original []byte,
	request Rewriter,
	response needlepath.Response,
	receipt Receipt,
) Decision {
	if mode != config.ModeQuarantine && mode != config.ModeEnforce {
		receipt.Reason = "sanitized_fallback_observed_not_enabled"
		return passThrough(original, receipt)
	}
	// From here on the server has said the raw context is unsafe to deliver, so
	// no branch may return the original body. Anything that cannot be delivered
	// as sanitized content is a Block.
	if strings.TrimSpace(response.RenderedContext) == "" {
		receipt.Reason = "empty_sanitized_fallback"
		return Decision{Kind: Block, Cause: CauseVerdict, Receipt: receipt}
	}
	if request.CandidateCount() != 1 {
		// A global rendered packet cannot be faithfully re-expanded into more
		// than one provider message. Restoring raw context would recreate the
		// batching bug this bridge is specifically designed to avoid.
		receipt.Reason = "sanitized_fallback_requires_record_mapping"
		return Decision{Kind: Block, Cause: CauseVerdict, Receipt: receipt}
	}
	body, err := request.RewriteSingleCandidate(response.RenderedContext)
	if err != nil {
		receipt.Reason = "sanitized_fallback_rewrite_failed"
		return Decision{Kind: Block, Cause: CauseVerdict, Receipt: receipt}
	}
	receipt.Reason = "sanitized_fallback"
	return Decision{Kind: Transform, Body: body, Receipt: receipt}
}

func passThrough(body []byte, receipt Receipt) Decision {
	return Decision{Kind: PassThrough, Body: body, Receipt: receipt}
}

func receiptFor(response needlepath.Response) Receipt {
	return Receipt{
		Outcome:         response.Outcome,
		TokensBefore:    response.TokensBefore,
		TokensAfter:     response.TokensAfter,
		TokensSaved:     response.TokensSaved,
		EngineLatencyMS: response.EngineLatencyMS,
		SelectionID:     response.SelectionID,
	}
}

func (d Decision) Error() error {
	if d.Kind != Block {
		return nil
	}
	return fmt.Errorf("Next Moca blocked unsafe context")
}
