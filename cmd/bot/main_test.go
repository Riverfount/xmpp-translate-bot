package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

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
// faria ao ser desligado.
type fakeXMPPClient struct {
	incoming chan xmpp.IncomingMessage
}

func newFakeXMPPClient(xmpp.Config, *slog.Logger) xmpp.Client {
	return &fakeXMPPClient{incoming: make(chan xmpp.IncomingMessage)}
}

func (f *fakeXMPPClient) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (f *fakeXMPPClient) SendGroup(string, string) error {
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
