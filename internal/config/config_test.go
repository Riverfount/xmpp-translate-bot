package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/config"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XMPP_JID", "tradutor@server.example")
	t.Setenv("XMPP_PASSWORD", "s3cret")
	t.Setenv("XMPP_SERVER", "server.example:5222")
	t.Setenv("XMPP_ROOMS", "sala@conference.server.example")
	t.Setenv("LT_URL", "http://lt.internal:5000")
	t.Setenv("LT_API_KEY", "lt-key")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"XMPP.TLS", cfg.XMPP.TLS, true},
		{"XMPP.Nickname", cfg.XMPP.Nickname, "tradutor"},
		{"LibreTranslate.TimeoutMs", cfg.LibreTranslate.TimeoutMs, 5000},
		{"LibreTranslate.MaxRetries", cfg.LibreTranslate.MaxRetries, 2},
		{"Translation.DefaultTarget", cfg.Translation.DefaultTarget, "pt"},
		{"Translation.Detector", cfg.Translation.Detector, "libretranslate"},
		{"Translation.MaxTextLength", cfg.Translation.MaxTextLength, 5000},
		{"Pipeline.Workers", cfg.Pipeline.Workers, 10},
		{"Pipeline.Queue", cfg.Pipeline.Queue, 100},
		{"Logging.Level", cfg.Logging.Level, "info"},
		{"Metrics.Addr", cfg.Metrics.Addr, ":9090"},
		{"Influx.Enabled", cfg.Influx.Enabled, false},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	tests := []struct {
		name  string
		unset string
	}{
		{"missing JID", "XMPP_JID"},
		{"missing password", "XMPP_PASSWORD"},
		{"missing server", "XMPP_SERVER"},
		{"missing rooms", "XMPP_ROOMS"},
		{"missing LT URL", "LT_URL"},
		{"missing LT API key", "LT_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(tt.unset, "")

			_, err := config.Load("")
			if err == nil {
				t.Fatalf("Load() error = nil, want error for %s unset", tt.unset)
			}
		})
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	setRequiredEnv(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	yamlContent := "xmpp:\n  jid: yaml-jid@server.example\n  nickname: yaml-nick\n"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Env wins for JID (set by setRequiredEnv); nickname comes from YAML since no env override.
	cfg, err := config.Load(yamlPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.XMPP.JID != "tradutor@server.example" {
		t.Errorf("XMPP.JID = %q, want env value to win over YAML", cfg.XMPP.JID)
	}
	if cfg.XMPP.Nickname != "yaml-nick" {
		t.Errorf("XMPP.Nickname = %q, want YAML value yaml-nick", cfg.XMPP.Nickname)
	}
}

func TestLoad_MalformedLTURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LT_URL", "not-a-url")

	_, err := config.Load("")
	if err == nil {
		t.Fatal("Load() error = nil, want error for malformed LT_URL")
	}
}

func TestLoad_MalformedTranslationPair(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("TRANSLATION_PAIRS", "en-pt")

	_, err := config.Load("")
	if err == nil {
		t.Fatal("Load() error = nil, want error for malformed TRANSLATION_PAIRS entry")
	}
}

func TestLoad_TranslationPairsParsed(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("TRANSLATION_PAIRS", "en:pt,es:pt,fr:pt,en:es")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := []config.LanguagePair{
		{Source: "en", Target: "pt"},
		{Source: "es", Target: "pt"},
		{Source: "fr", Target: "pt"},
		{Source: "en", Target: "es"},
	}
	if len(cfg.Translation.Pairs) != len(want) {
		t.Fatalf("Translation.Pairs = %v, want %v", cfg.Translation.Pairs, want)
	}
	for i, p := range want {
		if cfg.Translation.Pairs[i] != p {
			t.Errorf("Translation.Pairs[%d] = %v, want %v", i, cfg.Translation.Pairs[i], p)
		}
	}
}

func TestLoad_InfluxDisabledBySkipsValidation(t *testing.T) {
	setRequiredEnv(t)
	// INFLUX_ENABLED not set -> defaults to false -> no Influx fields required.

	_, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil when Influx disabled", err)
	}
}

func TestLoad_InfluxEnabledRequiresFields(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("INFLUX_ENABLED", "true")

	_, err := config.Load("")
	if err == nil {
		t.Fatal("Load() error = nil, want error when Influx enabled without URL/org/bucket/token")
	}
}

func TestLoad_InfluxEnabledWithFieldsSucceeds(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("INFLUX_ENABLED", "true")
	t.Setenv("INFLUX_URL", "http://influx.internal:8086")
	t.Setenv("INFLUX_ORG", "myorg")
	t.Setenv("INFLUX_BUCKET", "mybucket")
	t.Setenv("INFLUX_TOKEN", "influx-token")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !cfg.Influx.Enabled {
		t.Error("Influx.Enabled = false, want true")
	}
	if cfg.Influx.URL != "http://influx.internal:8086" {
		t.Errorf("Influx.URL = %q, want http://influx.internal:8086", cfg.Influx.URL)
	}
	if cfg.Influx.Token != "influx-token" {
		t.Errorf("Influx.Token = %q, want influx-token", cfg.Influx.Token)
	}
}

func TestLoad_YAMLNotFound(t *testing.T) {
	setRequiredEnv(t)

	_, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load() error = nil, want error for missing yaml file")
	}
}

func TestLoad_YAMLMalformed(t *testing.T) {
	setRequiredEnv(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte("xmpp: [this is not a map"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := config.Load(yamlPath)
	if err == nil {
		t.Fatal("Load() error = nil, want error for malformed yaml syntax")
	}
}

func TestLoad_YAMLTranslationPairs(t *testing.T) {
	setRequiredEnv(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	yamlContent := "translation:\n  pairs:\n    - en:pt\n    - es:pt\n"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := config.Load(yamlPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := []config.LanguagePair{{Source: "en", Target: "pt"}, {Source: "es", Target: "pt"}}
	if len(cfg.Translation.Pairs) != len(want) || cfg.Translation.Pairs[0] != want[0] || cfg.Translation.Pairs[1] != want[1] {
		t.Errorf("Translation.Pairs = %v, want %v", cfg.Translation.Pairs, want)
	}
}

func TestLoad_YAMLTranslationPairsMalformed(t *testing.T) {
	setRequiredEnv(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	yamlContent := "translation:\n  pairs:\n    - en-pt\n"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := config.Load(yamlPath)
	if err == nil {
		t.Fatal("Load() error = nil, want error for malformed yaml translation.pairs entry")
	}
}

func TestLoad_InvalidNumericOrBoolEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{"XMPP_TLS", "XMPP_TLS"},
		{"LT_TIMEOUT_MS", "LT_TIMEOUT_MS"},
		{"LT_MAX_RETRIES", "LT_MAX_RETRIES"},
		{"MAX_TEXT_LENGTH", "MAX_TEXT_LENGTH"},
		{"WORKER_POOL_SIZE", "WORKER_POOL_SIZE"},
		{"QUEUE_SIZE", "QUEUE_SIZE"},
		{"INFLUX_ENABLED", "INFLUX_ENABLED"},
		{"INFLUX_TIMEOUT_MS", "INFLUX_TIMEOUT_MS"},
		{"INFLUX_QUEUE_SIZE", "INFLUX_QUEUE_SIZE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(tt.env, "not-a-valid-value")

			_, err := config.Load("")
			if err == nil {
				t.Fatalf("Load() error = nil, want error for invalid %s", tt.env)
			}
		})
	}
}

func TestLoad_MaxTextLengthEnvOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MAX_TEXT_LENGTH", "1000")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.Translation.MaxTextLength != 1000 {
		t.Errorf("Translation.MaxTextLength = %d, want 1000", cfg.Translation.MaxTextLength)
	}
}

func TestLoad_InvalidDefaultTarget(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DEFAULT_TARGET", "")

	_, err := config.Load("")
	if err == nil {
		t.Fatal("Load() error = nil, want error for empty DEFAULT_TARGET")
	}
}
