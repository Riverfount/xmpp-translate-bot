package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
	"github.com/Riverfount/xmpp-translate-bot/internal/pipeline"
	"github.com/Riverfount/xmpp-translate-bot/internal/xmpp"
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

// fakeXMPPClient evita que os testes de run() dependam de uma conexão XMPP
// real: Start() só bloqueia até ctx ser cancelado, como o client de verdade
// faria ao ser desligado. SendGroup registra cada envio em sent, pra testar
// o wiring de dispatchIncoming sem precisar de uma sala de verdade.
type fakeXMPPClient struct {
	incoming chan xmpp.IncomingMessage
	sent     chan sentMessage
}

type sentMessage struct {
	room, body string
}

func newFakeXMPPClient(xmpp.Config, *slog.Logger, *observability.Metrics) xmpp.Client {
	return &fakeXMPPClient{
		incoming: make(chan xmpp.IncomingMessage),
		sent:     make(chan sentMessage, 10),
	}
}

func (f *fakeXMPPClient) Start(ctx context.Context) error {
	<-ctx.Done()
	close(f.incoming)
	return nil
}

func (f *fakeXMPPClient) SendGroup(room, body string) error {
	f.sent <- sentMessage{room, body}
	return nil
}

func (f *fakeXMPPClient) Incoming() <-chan xmpp.IncomingMessage {
	return f.incoming
}

func TestRun_FailsFastWithInvalidConfig(t *testing.T) {
	// Nenhuma variável obrigatória setada.
	var out bytes.Buffer
	if err := run(context.Background(), &out, newFakeXMPPClient); err == nil {
		t.Fatal("run() error = nil, want error for missing required config")
	}
}

func TestRun_SucceedsAndLogsStartupWithValidConfig(t *testing.T) {
	setRequiredEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	if err := run(ctx, &out, newFakeXMPPClient); err != nil {
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	if err := run(ctx, &out, newFakeXMPPClient); err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
}

type stubDetector struct{ lang string }

func (d stubDetector) Detect(context.Context, string) (string, float64, error) {
	return d.lang, 0.9, nil
}

type stubTranslator struct{ translated string }

func (t stubTranslator) Translate(context.Context, string, string, string) (string, error) {
	return t.translated, nil
}

func (t stubTranslator) SupportedLanguages(context.Context) ([]string, error) {
	return nil, nil
}

func newTestDispatcher(responder pipeline.Responder) *pipeline.Dispatcher {
	return pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    1,
		QueueSize:  1,
		Detector:   stubDetector{lang: "en"},
		Translator: stubTranslator{translated: "Olá"},
		Responder:  responder,
		Formatter:  pipeline.Formatter{Nickname: "tradutor", DefaultTarget: "pt"},
		JobTimeout: time.Second,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics:    observability.NewMetrics(prometheus.NewRegistry()),
	})
}

func TestDispatchIncoming_MentionedMessageReachesResponder(t *testing.T) {
	t.Parallel()

	fake := &fakeXMPPClient{incoming: make(chan xmpp.IncomingMessage, 1), sent: make(chan sentMessage, 1)}
	dispatcher := newTestDispatcher(fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Start(ctx)
	go dispatchIncoming(fake, "tradutor", dispatcher)

	fake.incoming <- xmpp.IncomingMessage{Room: "sala@conf", FromNick: "alice", Body: "@tradutor Hello"}

	select {
	case msg := <-fake.sent:
		want := "Tradução (en → pt): Olá"
		if msg.room != "sala@conf" || msg.body != want {
			t.Errorf("got %+v, want room=sala@conf body=%q", msg, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout esperando resposta do dispatcher")
	}
}

func TestDispatchIncoming_IgnoresSelfMessages(t *testing.T) {
	t.Parallel()

	fake := &fakeXMPPClient{incoming: make(chan xmpp.IncomingMessage, 1), sent: make(chan sentMessage, 1)}
	dispatcher := newTestDispatcher(fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Start(ctx)
	go dispatchIncoming(fake, "tradutor", dispatcher)

	fake.incoming <- xmpp.IncomingMessage{Room: "sala@conf", FromNick: "tradutor", Body: "@tradutor eco", IsSelf: true}

	select {
	case msg := <-fake.sent:
		t.Fatalf("envio inesperado para mensagem própria: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDispatchIncoming_IgnoresNonMentions(t *testing.T) {
	t.Parallel()

	fake := &fakeXMPPClient{incoming: make(chan xmpp.IncomingMessage, 1), sent: make(chan sentMessage, 1)}
	dispatcher := newTestDispatcher(fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Start(ctx)
	go dispatchIncoming(fake, "tradutor", dispatcher)

	fake.incoming <- xmpp.IncomingMessage{Room: "sala@conf", FromNick: "alice", Body: "hello sem menção"}

	select {
	case msg := <-fake.sent:
		t.Fatalf("envio inesperado para mensagem sem menção: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}
