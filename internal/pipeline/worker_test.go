package pipeline_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
	"github.com/Riverfount/xmpp-translate-bot/internal/pipeline"
	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeDetector struct {
	lang       string
	confidence float64
	err        error
}

func (f fakeDetector) Detect(context.Context, string) (string, float64, error) {
	return f.lang, f.confidence, f.err
}

// flakyDetector panica na primeira chamada e funciona normalmente depois —
// usado pra provar que o worker sobrevive a um panic e continua consumindo
// a fila.
type flakyDetector struct {
	calls int32
}

func (f *flakyDetector) Detect(context.Context, string) (string, float64, error) {
	if atomic.AddInt32(&f.calls, 1) == 1 {
		panic("boom")
	}
	return "en", 0.9, nil
}

type fakeTranslator struct {
	translated string
	err        error
}

func (f fakeTranslator) Translate(context.Context, string, string, string) (string, error) {
	return f.translated, f.err
}

func (f fakeTranslator) SupportedLanguages(context.Context) ([]string, error) {
	return nil, nil
}

type sentMessage struct {
	room, body string
}

// syncResponder publica cada SendGroup num canal, pra testes esperarem o job
// terminar sem sleep/poll.
type syncResponder struct {
	sent chan sentMessage
}

func newSyncResponder() *syncResponder {
	return &syncResponder{sent: make(chan sentMessage, 10)}
}

func (r *syncResponder) SendGroup(room, body string) error {
	r.sent <- sentMessage{room, body}
	return nil
}

func (r *syncResponder) awaitOne(t *testing.T) sentMessage {
	t.Helper()
	select {
	case msg := <-r.sent:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timeout esperando resposta do dispatcher")
		return sentMessage{}
	}
}

func newTestDispatcher(t *testing.T, detector translate.Detector, translator translate.Translator, responder pipeline.Responder) (*pipeline.Dispatcher, *observability.Metrics) {
	t.Helper()
	metrics := observability.NewMetrics(prometheus.NewRegistry())
	d := pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    1,
		QueueSize:  10,
		Detector:   detector,
		Translator: translator,
		Responder:  responder,
		Formatter: pipeline.Formatter{
			Nickname:      "tradutor",
			DefaultTarget: "pt",
		},
		JobTimeout: time.Second,
		Logger:     testLogger(),
		Metrics:    metrics,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Start(ctx)

	return d, metrics
}

func TestDispatcher_EmptyTextRespondsWithHelp(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t, fakeDetector{}, fakeTranslator{}, responder)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: ""})

	got := responder.awaitOne(t)
	want := "Use: @tradutor [sua mensagem]. Ex: @tradutor Hello world"
	if got.room != "sala@conf" || got.body != want {
		t.Errorf("got %+v, want room=sala@conf body=%q", got, want)
	}
}

func TestDispatcher_SuccessfulTranslation(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, metrics := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{translated: "Olá"},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	want := "Tradução (en → pt): Olá"
	if got.body != want {
		t.Errorf("body = %q, want %q", got.body, want)
	}

	if v := testutil.ToFloat64(metrics.TranslationsTotal.WithLabelValues("success", "en", "pt")); v != 1 {
		t.Errorf("translations_total{success,en,pt} = %v, want 1", v)
	}
}

func TestDispatcher_AlreadyAtTargetSkipsTranslation(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	translator := fakeTranslator{err: errors.New("Translate não deveria ser chamado")}
	d, metrics := newTestDispatcher(t,
		fakeDetector{lang: "pt", confidence: 0.95},
		translator,
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Já em português"})

	got := responder.awaitOne(t)
	want := "A mensagem já está em pt."
	if got.body != want {
		t.Errorf("body = %q, want %q", got.body, want)
	}

	if v := testutil.ToFloat64(metrics.TranslationsTotal.WithLabelValues("already_target", "pt", "pt")); v != 1 {
		t.Errorf("translations_total{already_target,pt,pt} = %v, want 1", v)
	}
}

func TestDispatcher_DetectErrorRespondsWithMappedMessage(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, metrics := newTestDispatcher(t,
		fakeDetector{err: &translate.HTTPError{StatusCode: 401}},
		fakeTranslator{},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	if got.body != pipeline.MsgAuthError {
		t.Errorf("body = %q, want %q", got.body, pipeline.MsgAuthError)
	}

	if v := testutil.ToFloat64(metrics.LibreTranslateErrorsTotal.WithLabelValues("auth")); v != 1 {
		t.Errorf("libretranslate_errors_total{auth} = %v, want 1", v)
	}
	if v := testutil.ToFloat64(metrics.TranslationsTotal.WithLabelValues("error", "", "")); v != 1 {
		t.Errorf("translations_total{error,\"\",\"\"} = %v, want 1", v)
	}
}

func TestDispatcher_TranslateErrorRespondsWithMappedMessage(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, metrics := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{err: &translate.HTTPError{StatusCode: 503}},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	if got.body != pipeline.MsgServiceUnavailable {
		t.Errorf("body = %q, want %q", got.body, pipeline.MsgServiceUnavailable)
	}

	if v := testutil.ToFloat64(metrics.LibreTranslateErrorsTotal.WithLabelValues("http")); v != 1 {
		t.Errorf("libretranslate_errors_total{http} = %v, want 1", v)
	}
}

func TestDispatcher_TextTooLongRejectsWithoutCallingTranslator(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	metrics := observability.NewMetrics(prometheus.NewRegistry())
	d := pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    1,
		QueueSize:  10,
		Detector:   fakeDetector{err: errors.New("Detect não deveria ser chamado")},
		Translator: fakeTranslator{err: errors.New("Translate não deveria ser chamado")},
		Responder:  responder,
		Formatter:  pipeline.Formatter{Nickname: "tradutor", DefaultTarget: "pt", MaxTextLength: 10},
		JobTimeout: time.Second,
		Logger:     testLogger(),
		Metrics:    metrics,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Start(ctx)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "isso aqui tem mais de dez caracteres"})

	got := responder.awaitOne(t)
	want := "Texto muito longo (máximo 10 caracteres)."
	if got.body != want {
		t.Errorf("body = %q, want %q", got.body, want)
	}
}

func TestDispatcher_TextWithinLimitProceedsNormally(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{translated: "Olá"},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	if got.body != "Tradução (en → pt): Olá" {
		t.Errorf("body = %q, texto dentro do limite não deveria ser rejeitado", got.body)
	}
}

func TestDispatcher_QueueFullDropsJobWithoutBlocking(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics(prometheus.NewRegistry())
	d := pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    0,
		QueueSize:  1,
		Detector:   fakeDetector{},
		Translator: fakeTranslator{},
		Responder:  newSyncResponder(),
		Formatter:  pipeline.Formatter{},
		JobTimeout: time.Second,
		Logger:     testLogger(),
		Metrics:    metrics,
	})

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "primeiro"})
	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "segundo, deve ser descartado"})

	if v := testutil.ToFloat64(metrics.QueueDroppedTotal); v != 1 {
		t.Errorf("queue_dropped_total = %v, want 1", v)
	}
}

func TestDispatcher_RecoversFromPanicAndKeepsProcessingNextJob(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	metrics := observability.NewMetrics(prometheus.NewRegistry())
	d := pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    1,
		QueueSize:  10,
		Detector:   &flakyDetector{},
		Translator: fakeTranslator{translated: "Olá"},
		Responder:  responder,
		Formatter:  pipeline.Formatter{Nickname: "tradutor", DefaultTarget: "pt"},
		JobTimeout: time.Second,
		Logger:     testLogger(),
		Metrics:    metrics,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Start(ctx)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "primeiro, panica no detect"})
	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "segundo, processa normal"})

	got := responder.awaitOne(t)
	want := "Tradução (en → pt): Olá"
	if got.body != want {
		t.Errorf("body = %q, want %q (job seguinte ao panic deveria processar normalmente)", got.body, want)
	}

	if v := testutil.ToFloat64(metrics.WorkerPoolActive); v != 0 {
		t.Errorf("worker_pool_active = %v, want 0 (Dec deve rodar mesmo com panic)", v)
	}
}

func TestDispatcher_StopDrainsQueuedJobsWithinDeadline(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{translated: "Olá"},
		responder,
	)

	const n = 5
	for i := 0; i < n; i++ {
		d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})
	}

	d.Stop(2 * time.Second)

	for i := 0; i < n; i++ {
		select {
		case <-responder.sent:
		default:
			t.Fatalf("job %d não foi drenado antes do Stop retornar", i)
		}
	}
}

func TestDispatcher_StopRejectsSubmitAfterCalled(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t, fakeDetector{lang: "en"}, fakeTranslator{translated: "Olá"}, responder)

	d.Stop(time.Second)
	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "depois do shutdown"})

	select {
	case msg := <-responder.sent:
		t.Fatalf("job submetido depois de Stop() não deveria ser processado: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDispatcher_StopReturnsAfterDeadlineEvenWithStuckJob(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	metrics := observability.NewMetrics(prometheus.NewRegistry())
	d := pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    1,
		QueueSize:  10,
		Detector:   blockingDetector{block: block},
		Translator: fakeTranslator{},
		Responder:  newSyncResponder(),
		Formatter:  pipeline.Formatter{Nickname: "tradutor", DefaultTarget: "pt"},
		JobTimeout: time.Minute,
		Logger:     testLogger(),
		Metrics:    metrics,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Start(ctx)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "vai travar"})
	time.Sleep(20 * time.Millisecond) // garante que o worker já pegou o job

	start := time.Now()
	d.Stop(50 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("Stop() levou %v, want próximo de 50ms (não deveria esperar o job travado)", elapsed)
	}
}

type blockingDetector struct {
	block chan struct{}
}

func (b blockingDetector) Detect(ctx context.Context, _ string) (string, float64, error) {
	select {
	case <-b.block:
	case <-ctx.Done():
	}
	return "", 0, ctx.Err()
}

func TestDispatcher_SlowInfluxEndpointDoesNotDelayJobResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	metrics := observability.NewMetrics(prometheus.NewRegistry())
	influxWriter := observability.NewInfluxWriter(observability.InfluxWriterConfig{
		URL:       srv.URL,
		Org:       "org",
		Bucket:    "bucket",
		Token:     "token",
		Timeout:   time.Second,
		QueueSize: 10,
	}, testLogger(), metrics)
	t.Cleanup(influxWriter.Close)

	responder := newSyncResponder()
	d := pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    1,
		QueueSize:  10,
		Detector:   fakeDetector{lang: "en", confidence: 0.9},
		Translator: fakeTranslator{translated: "Olá"},
		Responder:  responder,
		Formatter:  pipeline.Formatter{Nickname: "tradutor", DefaultTarget: "pt"},
		JobTimeout: time.Second,
		Logger:     testLogger(),
		Metrics:    metrics,
		Influx:     influxWriter,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Start(ctx)

	start := time.Now()
	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})
	got := responder.awaitOne(t)
	elapsed := time.Since(start)

	if got.body != "Tradução (en → pt): Olá" {
		t.Errorf("body = %q", got.body)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("resposta levou %v, want bem menor que o delay do InfluxDB (300ms) — Enqueue não deveria bloquear o job", elapsed)
	}
}

func TestJobTimeout_DerivesFromLTTimeoutAndMaxRetries(t *testing.T) {
	t.Parallel()

	got := pipeline.JobTimeout(5*time.Second, 2)
	want := 15 * time.Second
	if got != want {
		t.Errorf("JobTimeout(5s, 2) = %v, want %v", got, want)
	}
}
