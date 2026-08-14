package pipeline_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

func TestJobTimeout_DerivesFromLTTimeoutAndMaxRetries(t *testing.T) {
	t.Parallel()

	got := pipeline.JobTimeout(5*time.Second, 2)
	want := 15 * time.Second
	if got != want {
		t.Errorf("JobTimeout(5s, 2) = %v, want %v", got, want)
	}
}
