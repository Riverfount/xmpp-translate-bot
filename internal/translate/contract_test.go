package translate_test

// Fixtures de contrato em testdata/ documentam o formato exato dos payloads
// de /detect, /translate e /languages (spec técnica §8) como dados, não como
// literais espalhados pelos testes — servem tanto pra verificar o que o
// client envia quanto o que ele precisa saber parsear.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("lendo fixture %q: %v", name, err)
	}
	return data
}

func decodeFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(loadFixture(t, name), &m); err != nil {
		t.Fatalf("decodificando fixture %q: %v", name, err)
	}
	return m
}

func TestContract_Detect_RequestMatchesFixture(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "detect_response.json"))
	}))
	t.Cleanup(srv.Close)

	client := translate.NewClient(srv.URL, "test-key", 0, 0, testLogger())
	if _, _, err := client.Detect(context.Background(), "Hello, how are you?"); err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	want := decodeFixture(t, "detect_request.json")
	for k, v := range want {
		if gotBody[k] != v {
			t.Errorf("request[%q] = %v, want %v (fixture detect_request.json)", k, gotBody[k], v)
		}
	}
}

func TestContract_Detect_ParsesResponseFixture(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "detect_response.json"))
	}))
	t.Cleanup(srv.Close)

	client := translate.NewClient(srv.URL, "test-key", 0, 0, testLogger())
	lang, confidence, err := client.Detect(context.Background(), "Hello, how are you?")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if lang != "en" {
		t.Errorf("lang = %q, want %q (fixture detect_response.json)", lang, "en")
	}
	if confidence < 0.966 || confidence > 0.968 {
		t.Errorf("confidence = %v, want ~0.967 (fixture detect_response.json: 96.7)", confidence)
	}
}

func TestContract_Translate_RequestMatchesFixture(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "translate_response.json"))
	}))
	t.Cleanup(srv.Close)

	client := translate.NewClient(srv.URL, "test-key", 0, 0, testLogger())
	if _, err := client.Translate(context.Background(), "Hello, how are you?", "en", "pt"); err != nil {
		t.Fatalf("Translate() error = %v", err)
	}

	want := decodeFixture(t, "translate_request.json")
	for k, v := range want {
		if gotBody[k] != v {
			t.Errorf("request[%q] = %v, want %v (fixture translate_request.json)", k, gotBody[k], v)
		}
	}
}

func TestContract_Translate_ParsesResponseFixture(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "translate_response.json"))
	}))
	t.Cleanup(srv.Close)

	client := translate.NewClient(srv.URL, "test-key", 0, 0, testLogger())
	got, err := client.Translate(context.Background(), "Hello, how are you?", "en", "pt")
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	want := "Olá, como você está?"
	if got != want {
		t.Errorf("Translate() = %q, want %q (fixture translate_response.json)", got, want)
	}
}

func TestContract_SupportedLanguages_ParsesResponseFixture(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/languages" {
			t.Errorf("request = %s %s, want GET /languages", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "languages_response.json"))
	}))
	t.Cleanup(srv.Close)

	client := translate.NewClient(srv.URL, "test-key", 0, 0, testLogger())
	codes, err := client.SupportedLanguages(context.Background())
	if err != nil {
		t.Fatalf("SupportedLanguages() error = %v", err)
	}
	want := []string{"en"}
	if len(codes) != 1 || codes[0] != want[0] {
		t.Errorf("SupportedLanguages() = %v, want %v (fixture languages_response.json)", codes, want)
	}
}
