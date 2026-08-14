package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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

func TestRun_FailsFastWithInvalidConfig(t *testing.T) {
	// Nenhuma variável obrigatória setada.
	var out bytes.Buffer
	if err := run(&out); err == nil {
		t.Fatal("run() error = nil, want error for missing required config")
	}
}

func TestRun_SucceedsAndLogsStartupWithValidConfig(t *testing.T) {
	setRequiredEnv(t)

	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}

	var found bool
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not valid JSON: %v\nline: %s", err, line)
		}
		if entry["msg"] == "bot_starting" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a bot_starting log line, got: %s", out.String())
	}
}

func TestRun_WithInfluxEnabledCreatesWriterWithoutError(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("INFLUX_ENABLED", "true")
	t.Setenv("INFLUX_URL", "http://influx.internal:8086")
	t.Setenv("INFLUX_ORG", "org")
	t.Setenv("INFLUX_BUCKET", "bucket")
	t.Setenv("INFLUX_TOKEN", "tok")

	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
}
