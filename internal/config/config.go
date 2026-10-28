package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultEndpoint                    = "https://api.nextmoca.com"
	DefaultMaxContextTokens            = 8192
	DefaultCapacityTokens              = 128000
	DefaultTimeoutMilliseconds         = 3000
	DefaultBreakerFailures             = 5
	DefaultBreakerCooldownMilliseconds = 30000
	DefaultAutochunk                   = true
)

type Mode string

const (
	ModeShadow     Mode = "shadow"
	ModeActive     Mode = "active"
	ModeQuarantine Mode = "quarantine"
	ModeEnforce    Mode = "enforce"
)

// Screens reports whether the mode applies Next Moca's security verdicts to
// delivery. Shadow only observes and active is selection-only.
func (m Mode) Screens() bool {
	return m == ModeQuarantine || m == ModeEnforce
}

// Action is the configured response to a condition the plugin cannot govern.
// The zero value behaves as ActionPass.
type Action string

const (
	ActionPass  Action = "pass"
	ActionBlock Action = "block"
)

type Config struct {
	Endpoint         string
	APIKey           string
	APIKeyEnv        string
	Mode             Mode
	OperatingPoint   string
	MaxContextTokens int
	CapacityTokens   int
	Timeout          time.Duration
	TelemetryEnabled bool
	// VerifyLiteralExcerpts rejects a selected hosted excerpt unless it occurs
	// verbatim in its original candidate message. It is opt-in because some
	// compatible selection services normalize whitespace before returning text.
	VerifyLiteralExcerpts bool
	// Autochunk asks the hosted service to split oversized candidate messages
	// before selection. The plugin maps selected child excerpts back to their
	// original provider message before forwarding the request.
	Autochunk bool

	// OnSelectionError and UnsupportedShape apply in quarantine and enforce.
	OnSelectionError Action
	UnsupportedShape Action

	// BreakerFailures consecutive availability failures open the circuit
	// breaker for BreakerCooldown. A value of zero disables the breaker.
	BreakerFailures int
	BreakerCooldown time.Duration
}

func Parse(values map[string]string, lookupEnv func(string) string) (Config, error) {
	if lookupEnv == nil {
		return Config{}, fmt.Errorf("environment lookup is required")
	}

	mode, err := parseMode(values["mode"])
	if err != nil {
		return Config{}, err
	}

	endpoint, err := parseEndpoint(valueOr(values["endpoint"], DefaultEndpoint))
	if err != nil {
		return Config{}, err
	}

	operatingPoint := strings.TrimSpace(values["operating_point"])
	if operatingPoint == "" {
		return Config{}, fmt.Errorf("operating_point is required; pin an immutable Needlepath label")
	}

	apiKey := strings.TrimSpace(values["api_key"])
	apiKeyEnv := strings.TrimSpace(values["api_key_env"])
	if apiKey == "" && apiKeyEnv != "" {
		apiKey = strings.TrimSpace(lookupEnv(apiKeyEnv))
	}
	if apiKey == "" {
		return Config{}, fmt.Errorf("api_key or api_key_env must provide a Needlepath API key")
	}

	maxContextTokens, err := parsePositiveInt(values, "max_context_tokens", DefaultMaxContextTokens)
	if err != nil {
		return Config{}, err
	}
	capacityTokens, err := parsePositiveInt(values, "capacity_tokens", DefaultCapacityTokens)
	if err != nil {
		return Config{}, err
	}
	if capacityTokens < maxContextTokens {
		return Config{}, fmt.Errorf("capacity_tokens must be greater than or equal to max_context_tokens")
	}
	timeoutMilliseconds, err := parsePositiveInt(values, "timeout_ms", DefaultTimeoutMilliseconds)
	if err != nil {
		return Config{}, err
	}
	telemetryEnabled, err := parseBool(values, "telemetry_enabled", false)
	if err != nil {
		return Config{}, err
	}
	verifyLiteralExcerpts, err := parseBool(values, "verify_literal_excerpts", false)
	if err != nil {
		return Config{}, err
	}
	autochunk, err := parseBool(values, "autochunk", DefaultAutochunk)
	if err != nil {
		return Config{}, err
	}
	onSelectionError, err := parseAction(values, "on_selection_error")
	if err != nil {
		return Config{}, err
	}
	unsupportedShape, err := parseAction(values, "unsupported_shape")
	if err != nil {
		return Config{}, err
	}
	breakerFailures, err := parsePositiveInt(values, "breaker_failures", DefaultBreakerFailures)
	if err != nil {
		return Config{}, err
	}
	breakerCooldownMilliseconds, err := parsePositiveInt(values, "breaker_cooldown_ms", DefaultBreakerCooldownMilliseconds)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Endpoint:              endpoint,
		APIKey:                apiKey,
		APIKeyEnv:             apiKeyEnv,
		Mode:                  mode,
		OperatingPoint:        operatingPoint,
		MaxContextTokens:      maxContextTokens,
		CapacityTokens:        capacityTokens,
		Timeout:               time.Duration(timeoutMilliseconds) * time.Millisecond,
		TelemetryEnabled:      telemetryEnabled,
		VerifyLiteralExcerpts: verifyLiteralExcerpts,
		Autochunk:             autochunk,
		OnSelectionError:      onSelectionError,
		UnsupportedShape:      unsupportedShape,
		BreakerFailures:       breakerFailures,
		BreakerCooldown:       time.Duration(breakerCooldownMilliseconds) * time.Millisecond,
	}, nil
}

// InvalidConfigurationFailsOpen reports whether a malformed startup
// configuration belongs to a non-screening trial mode. The value is used only
// after Parse fails, when a complete Config cannot exist. Unknown modes remain
// fail-closed rather than silently disabling a requested security policy.
func InvalidConfigurationFailsOpen(values map[string]string) bool {
	switch Mode(strings.ToLower(strings.TrimSpace(values["mode"]))) {
	case "", ModeShadow, ModeActive:
		return true
	default:
		return false
	}
}

func parseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		return ModeShadow, nil
	}
	switch mode {
	case ModeShadow, ModeActive, ModeQuarantine, ModeEnforce:
		return mode, nil
	default:
		return "", fmt.Errorf("mode must be shadow, active, quarantine, or enforce")
	}
}

func parseAction(values map[string]string, key string) (Action, error) {
	action := Action(strings.ToLower(strings.TrimSpace(values[key])))
	switch action {
	case "":
		return ActionPass, nil
	case ActionPass, ActionBlock:
		return action, nil
	default:
		return "", fmt.Errorf("%s must be pass or block", key)
	}
}

func parseEndpoint(value string) (string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("endpoint must be an absolute HTTPS URL")
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("endpoint must use HTTPS")
	}
	return endpoint, nil
}

func parsePositiveInt(values map[string]string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}

func parseBool(values map[string]string, key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return value, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
