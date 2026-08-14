package observability_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

func TestNewLogger_ValidLevels(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"debug", "info", "warn", "error", "DEBUG", "Info"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if _, err := observability.NewLogger(&buf, level); err != nil {
				t.Fatalf("NewLogger(%q) error = %v, want nil", level, err)
			}
		})
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	_, err := observability.NewLogger(&buf, "bogus")
	if err == nil {
		t.Fatal("NewLogger() error = nil, want error for invalid level")
	}
}

func TestNewLogger_EmitsJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger, err := observability.NewLogger(&buf, "info")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	logger.Info("translation_completed", "room", "sala@conference.server", "status", "success")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if entry["msg"] != "translation_completed" {
		t.Errorf("msg = %v, want translation_completed", entry["msg"])
	}
	if entry["room"] != "sala@conference.server" {
		t.Errorf("room = %v, want sala@conference.server", entry["room"])
	}
	if entry["status"] != "success" {
		t.Errorf("status = %v, want success", entry["status"])
	}
}

func TestNewLogger_FiltersBelowConfiguredLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger, err := observability.NewLogger(&buf, "warn")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	logger.Info("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for Info() below configured warn level, got: %s", buf.String())
	}

	logger.Warn("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Fatalf("expected Warn() output to appear, got: %s", buf.String())
	}
}
