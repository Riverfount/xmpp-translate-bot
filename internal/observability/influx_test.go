package observability_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

func newTestWriter(t *testing.T, serverURL string, queueSize int) (*observability.InfluxWriter, *observability.Metrics) {
	t.Helper()
	reg := prometheus.NewRegistry()
	metrics := observability.NewMetrics(reg)
	logger, err := observability.NewLogger(&bytes.Buffer{}, "debug")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	writer := observability.NewInfluxWriter(observability.InfluxWriterConfig{
		URL:       serverURL,
		Org:       "SUA_ORG",
		Bucket:    "MEU_BUCKET",
		Token:     "SEU_TOKEN_AQUI",
		Timeout:   time.Second,
		QueueSize: queueSize,
	}, logger, metrics)
	return writer, metrics
}

func TestInfluxWriter_SendsLineProtocolMatchingReferenceFormat(t *testing.T) {
	t.Parallel()
	type captured struct {
		method      string
		path        string
		query       string
		authHeader  string
		contentType string
		body        string
	}
	var got captured
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = captured{
			method:      r.Method,
			path:        r.URL.Path,
			query:       r.URL.RawQuery,
			authHeader:  r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
			body:        string(body),
		}
		w.WriteHeader(http.StatusNoContent)
		close(done)
	}))
	defer srv.Close()

	writer, _ := newTestWriter(t, srv.URL, 10)
	writer.Enqueue(observability.TranslationEvent{
		SrcLang:  "fr",
		DstLang:  "pt-BR",
		MUC:      "nome_canal",
		Status:   "success",
		Duration: 1345 * time.Millisecond,
	})
	writer.Close()
	<-done

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.path != "/api/v2/write" {
		t.Errorf("path = %q, want /api/v2/write", got.path)
	}
	query, err := url.ParseQuery(got.query)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", got.query, err)
	}
	for k, want := range map[string]string{"org": "SUA_ORG", "bucket": "MEU_BUCKET", "precision": "s"} {
		if got := query.Get(k); got != want {
			t.Errorf("query[%q] = %q, want %q", k, got, want)
		}
	}
	if got.authHeader != "Token SEU_TOKEN_AQUI" {
		t.Errorf("Authorization = %q, want %q", got.authHeader, "Token SEU_TOKEN_AQUI")
	}
	if got.contentType != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got.contentType)
	}
	wantBody := "transbot,src_lang=fr,dst_lang=pt-BR,muc=nome_canal,status=success duration=1.345"
	if got.body != wantBody {
		t.Errorf("body = %q, want %q", got.body, wantBody)
	}
}

func TestInfluxWriter_EscapesTagValues(t *testing.T) {
	t.Parallel()
	bodies := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	writer, _ := newTestWriter(t, srv.URL, 10)
	writer.Enqueue(observability.TranslationEvent{
		SrcLang:  "en",
		DstLang:  "pt",
		MUC:      `sala de testes,x=1\y`,
		Status:   "success",
		Duration: time.Second,
	})
	writer.Close()

	body := <-bodies
	want := `transbot,src_lang=en,dst_lang=pt,muc=sala\ de\ testes\,x\=1\\y,status=success duration=1`
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestInfluxWriter_HTTPErrorIncrementsMetricNoRetry(t *testing.T) {
	t.Parallel()
	var reqCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	writer, metrics := newTestWriter(t, srv.URL, 10)
	writer.Enqueue(observability.TranslationEvent{SrcLang: "en", DstLang: "pt", MUC: "sala", Status: "success", Duration: time.Millisecond})
	writer.Close()

	if got := atomic.LoadInt32(&reqCount); got != 1 {
		t.Errorf("server received %d requests, want exactly 1 (no retry)", got)
	}
	if got := testutil.ToFloat64(metrics.InfluxWriteErrorsTotal); got != 1 {
		t.Errorf("InfluxWriteErrorsTotal = %v, want 1", got)
	}
}

func TestInfluxWriter_QueueFullDropsAndIncrementsMetricWithoutBlocking(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	var reqCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	writer, metrics := newTestWriter(t, srv.URL, 1)

	writer.Enqueue(observability.TranslationEvent{SrcLang: "en", DstLang: "pt", MUC: "sala", Status: "success", Duration: time.Millisecond})
	<-started // worker agora está bloqueado enviando o request #1

	writer.Enqueue(observability.TranslationEvent{SrcLang: "es", DstLang: "pt", MUC: "sala", Status: "success", Duration: time.Millisecond}) // ocupa a fila (cap 1)
	writer.Enqueue(observability.TranslationEvent{SrcLang: "fr", DstLang: "pt", MUC: "sala", Status: "success", Duration: time.Millisecond}) // fila cheia -> dropado

	if got := testutil.ToFloat64(metrics.InfluxWriteErrorsTotal); got != 1 {
		t.Fatalf("InfluxWriteErrorsTotal = %v, want 1 (drop imediato, sem bloquear)", got)
	}

	release <- struct{}{}
	<-started
	release <- struct{}{}

	writer.Close()

	if got := atomic.LoadInt32(&reqCount); got != 2 {
		t.Errorf("server received %d requests, want 2 (evt1 + evt2, evt3 foi dropado)", got)
	}
}
