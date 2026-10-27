package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseResolvesAPIKeyFromConfiguredEnvironmentVariable(t *testing.T) {
	cfg, err := Parse(
		map[string]string{
			"api_key_env":     "NEXTMOCA_API_KEY",
			"operating_point": "np-2026-10-r7",
		},
		func(key string) string {
			if key == "NEXTMOCA_API_KEY" {
				return "np_live_test"
			}
			return ""
		},
	)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.APIKey != "np_live_test" {
		t.Fatalf("Config.APIKey = %q, want resolved key", cfg.APIKey)
	}
	if cfg.Mode != ModeShadow {
		t.Fatalf("Config.Mode = %q, want %q", cfg.Mode, ModeShadow)
	}
}

func TestParseRejectsEnforceModeWithoutAPIKey(t *testing.T) {
	_, err := Parse(
		map[string]string{
			"mode":            string(ModeEnforce),
			"operating_point": "np-2026-10-r7",
		},
		func(string) string { return "" },
	)
	if err == nil {
		t.Fatal("Parse() error = nil, want missing API-key error")
	}
	if !strings.Contains(err.Error(), "api_key_env") {
		t.Fatalf("Parse() error = %q, want api_key_env guidance", err)
	}
}

func TestParseRejectsUnknownMode(t *testing.T) {
	_, err := Parse(
		map[string]string{
			"mode":            "turbo",
			"api_key_env":     "NEXTMOCA_API_KEY",
			"operating_point": "np-2026-10-r7",
		},
		func(string) string { return "np_live_test" },
	)
	if err == nil {
		t.Fatal("Parse() error = nil, want unknown mode error")
	}
}

func TestParseRejectsMissingOperatingPoint(t *testing.T) {
	_, err := Parse(
		map[string]string{"api_key_env": "NEXTMOCA_API_KEY"},
		func(string) string { return "np_live_test" },
	)
	if err == nil {
		t.Fatal("Parse() error = nil, want missing operating point error")
	}
	if !strings.Contains(err.Error(), "operating_point") {
		t.Fatalf("Parse() error = %q, want operating_point guidance", err)
	}
}

func TestParseDefaultsFailOpenWithABoundedTimeoutAndABreaker(t *testing.T) {
	cfg, err := Parse(
		map[string]string{
			"api_key_env":     "NEXTMOCA_API_KEY",
			"operating_point": "np-2026-10-r8",
		},
		func(string) string { return "np_live_test" },
	)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Timeout != 3*time.Second {
		t.Fatalf("Config.Timeout = %s, want 3s", cfg.Timeout)
	}
	if cfg.OnSelectionError != ActionPass || cfg.UnsupportedShape != ActionPass {
		t.Fatalf("actions = (%q, %q), want pass for both", cfg.OnSelectionError, cfg.UnsupportedShape)
	}
	if cfg.BreakerFailures != 5 || cfg.BreakerCooldown != 30*time.Second {
		t.Fatalf("breaker = (%d, %s), want (5, 30s)", cfg.BreakerFailures, cfg.BreakerCooldown)
	}
}

func TestParseAcceptsBlockActionsAndBreakerSettings(t *testing.T) {
	cfg, err := Parse(
		map[string]string{
			"api_key_env":         "NEXTMOCA_API_KEY",
			"operating_point":     "np-2026-10-r8",
			"on_selection_error":  "Block",
			"unsupported_shape":   "block",
			"timeout_ms":          "1500",
			"breaker_failures":    "3",
			"breaker_cooldown_ms": "10000",
		},
		func(string) string { return "np_live_test" },
	)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.OnSelectionError != ActionBlock || cfg.UnsupportedShape != ActionBlock {
		t.Fatalf("actions = (%q, %q), want block for both", cfg.OnSelectionError, cfg.UnsupportedShape)
	}
	if cfg.Timeout != 1500*time.Millisecond || cfg.BreakerFailures != 3 || cfg.BreakerCooldown != 10*time.Second {
		t.Fatalf("cfg = %#v", cfg)
	}
}

func TestParseRejectsUnknownActions(t *testing.T) {
	for _, key := range []string{"on_selection_error", "unsupported_shape"} {
		t.Run(key, func(t *testing.T) {
			_, err := Parse(
				map[string]string{
					"api_key_env":     "NEXTMOCA_API_KEY",
					"operating_point": "np-2026-10-r8",
					key:               "ignore",
				},
				func(string) string { return "np_live_test" },
			)
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("Parse() error = %v, want %s guidance", err, key)
			}
		})
	}
}

func TestModeScreensOnlyInQuarantineAndEnforce(t *testing.T) {
	want := map[Mode]bool{ModeShadow: false, ModeActive: false, ModeQuarantine: true, ModeEnforce: true}
	for mode, screens := range want {
		if mode.Screens() != screens {
			t.Fatalf("%s.Screens() = %v, want %v", mode, mode.Screens(), screens)
		}
	}
}
