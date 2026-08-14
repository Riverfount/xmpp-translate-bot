package translate_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func noSleep(context.Context, time.Duration) error { return nil }

func newTestClient(t *testing.T, handler http.HandlerFunc, maxRetries int) *translate.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return translate.NewClient(srv.URL, "test-key", time.Second, maxRetries, testLogger(), translate.WithSleepFunc(noSleep))
}

func TestTranslate_SendsExpectedRequestAndParsesResponse(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	var gotPath, gotMethod, gotContentType string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"translatedText": "Olá"})
	}, 2)

	got, err := client.Translate(context.Background(), "Hello", "en", "pt")
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got != "Olá" {
		t.Errorf("Translate() = %q, want %q", got, "Olá")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/translate" {
		t.Errorf("path = %q, want /translate", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["q"] != "Hello" || gotBody["source"] != "en" || gotBody["target"] != "pt" ||
		gotBody["format"] != "text" || gotBody["api_key"] != "test-key" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestTranslate_EmptySourceSendsAuto(t *testing.T) {
	t.Parallel()

	var gotSource string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotSource, _ = body["source"].(string)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"translatedText": "x"})
	}, 0)

	if _, err := client.Translate(context.Background(), "hi", "", "pt"); err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if gotSource != "auto" {
		t.Errorf("source enviado = %q, want %q", gotSource, "auto")
	}
}

func TestDetect_ReturnsHighestConfidenceNormalizedTo0And1(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"confidence": 40.0, "language": "es"},
			{"confidence": 96.7, "language": "en"},
		})
	}, 0)

	lang, confidence, err := client.Detect(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if lang != "en" {
		t.Errorf("lang = %q, want %q", lang, "en")
	}
	if confidence < 0.966 || confidence > 0.968 {
		t.Errorf("confidence = %v, want ~0.967", confidence)
	}
}

func TestDetect_EmptyResultIsError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}, 0)

	if _, _, err := client.Detect(context.Background(), "Hello"); err == nil {
		t.Error("Detect() error = nil, want error for empty result")
	}
}

func TestSupportedLanguages_ParsesCodes(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/languages" {
			t.Errorf("path = %q, want /languages", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"code": "en", "name": "English", "targets": []string{"pt", "es"}},
			{"code": "pt", "name": "Portuguese", "targets": []string{"en"}},
		})
	}, 0)

	codes, err := client.SupportedLanguages(context.Background())
	if err != nil {
		t.Fatalf("SupportedLanguages() error = %v", err)
	}
	want := []string{"en", "pt"}
	if !slices.Equal(codes, want) {
		t.Errorf("SupportedLanguages() = %v, want %v", codes, want)
	}
}

func TestDoJSON_400DoesNotRetry(t *testing.T) {
	t.Parallel()

	var calls int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}, 2)

	if _, err := client.Translate(context.Background(), "hi", "en", "pt"); err == nil {
		t.Fatal("Translate() error = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (sem retry em 400)", got)
	}
}

func TestDoJSON_401DoesNotRetry(t *testing.T) {
	t.Parallel()

	var calls int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}, 2)

	if _, err := client.Translate(context.Background(), "hi", "en", "pt"); err == nil {
		t.Fatal("Translate() error = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (sem retry em 401)", got)
	}
}

func TestDoJSON_403DoesNotRetry(t *testing.T) {
	t.Parallel()

	var calls int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusForbidden)
	}, 2)

	if _, err := client.Translate(context.Background(), "hi", "en", "pt"); err == nil {
		t.Fatal("Translate() error = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (sem retry em 403)", got)
	}
}

func TestDoJSON_429RetriesUntilExhausted(t *testing.T) {
	t.Parallel()

	var calls int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}, 2)

	if _, err := client.Translate(context.Background(), "hi", "en", "pt"); err == nil {
		t.Fatal("Translate() error = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3 (1 + maxRetries)", got)
	}
}

func TestDoJSON_5xxRetriesUntilExhausted(t *testing.T) {
	t.Parallel()

	var calls int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}, 2)

	if _, err := client.Translate(context.Background(), "hi", "en", "pt"); err == nil {
		t.Fatal("Translate() error = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3 (1 + maxRetries)", got)
	}
}

func TestDoJSON_SucceedsAfterTransientRetry(t *testing.T) {
	t.Parallel()

	var calls int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"translatedText": "ok"})
	}, 2)

	got, err := client.Translate(context.Background(), "hi", "en", "pt")
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got != "ok" {
		t.Errorf("Translate() = %q, want %q", got, "ok")
	}
	if calls := atomic.LoadInt32(&calls); calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestDoJSON_NetworkErrorIsRetried(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close() // porta fechada -> connection refused

	client := translate.NewClient(addr, "key", 200*time.Millisecond, 1, testLogger(), translate.WithSleepFunc(noSleep))
	if _, err := client.Translate(context.Background(), "hi", "en", "pt"); err == nil {
		t.Fatal("Translate() error = nil, want error (connection refused)")
	}
}

func TestDoJSON_AbortsWhenContextCanceledDuringBackoff(t *testing.T) {
	t.Parallel()

	var calls int32
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	sleepThenCancel := func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	client := translate.NewClient(srv.URL, "key", time.Second, 5, testLogger(), translate.WithSleepFunc(sleepThenCancel))

	if _, err := client.Translate(ctx, "hi", "en", "pt"); err == nil {
		t.Fatal("Translate() error = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (abortado durante o backoff)", got)
	}
}
