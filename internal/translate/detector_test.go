package translate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

func TestNewDetector_Libretranslate(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"confidence": 90.0, "language": "en"}})
	}))
	t.Cleanup(srv.Close)

	client := translate.NewClient(srv.URL, "key", time.Second, 0, testLogger())
	d, err := translate.NewDetector("libretranslate", client)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}

	lang, _, err := d.Detect(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if lang != "en" {
		t.Errorf("Detect() lang = %q, want %q", lang, "en")
	}
}

func TestNewDetector_DefaultsToLibretranslateWhenEmpty(t *testing.T) {
	t.Parallel()

	client := translate.NewClient("http://example.invalid", "key", time.Second, 0, testLogger())
	d, err := translate.NewDetector("", client)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}
	if d == nil {
		t.Fatal("NewDetector() returned nil Detector")
	}
}

func TestNewDetector_LocalIsNotImplemented(t *testing.T) {
	t.Parallel()

	client := translate.NewClient("http://example.invalid", "key", time.Second, 0, testLogger())
	if _, err := translate.NewDetector("local", client); err == nil {
		t.Error(`NewDetector("local") error = nil, want error (não implementado)`)
	}
}

func TestNewDetector_UnknownKindErrors(t *testing.T) {
	t.Parallel()

	client := translate.NewClient("http://example.invalid", "key", time.Second, 0, testLogger())
	if _, err := translate.NewDetector("bogus", client); err == nil {
		t.Error(`NewDetector("bogus") error = nil, want error`)
	}
}
