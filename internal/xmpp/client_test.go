package xmpp

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"gosrc.io/xmpp/stanza"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeSender struct {
	sent []stanza.Packet
}

func (f *fakeSender) Send(p stanza.Packet) error {
	f.sent = append(f.sent, p)
	return nil
}

func (f *fakeSender) SendIQ(_ context.Context, _ *stanza.IQ) (chan stanza.IQ, error) {
	return nil, nil
}

func (f *fakeSender) SendRaw(_ string) error {
	return nil
}

func TestJoinRooms_SendsPresenceForEachConfiguredRoom(t *testing.T) {
	t.Parallel()

	c := &client{
		cfg: Config{
			Rooms:    []string{"sala1@conference.example", "sala2@conference.example"},
			Nickname: "tradutor",
		},
		logger: testLogger(),
	}

	fake := &fakeSender{}
	c.joinRooms(fake)

	if len(fake.sent) != 2 {
		t.Fatalf("len(sent) = %d, want 2", len(fake.sent))
	}

	wantTo := []string{"sala1@conference.example/tradutor", "sala2@conference.example/tradutor"}
	for i, want := range wantTo {
		pres, ok := fake.sent[i].(stanza.Presence)
		if !ok {
			t.Fatalf("sent[%d] type = %T, want stanza.Presence", i, fake.sent[i])
		}
		if pres.To != want {
			t.Errorf("sent[%d].To = %q, want %q", i, pres.To, want)
		}
		if len(pres.Extensions) != 1 {
			t.Fatalf("sent[%d] Extensions = %d, want 1", i, len(pres.Extensions))
		}
		muc, ok := pres.Extensions[0].(stanza.MucPresence)
		if !ok {
			t.Fatalf("sent[%d] extension type = %T, want stanza.MucPresence", i, pres.Extensions[0])
		}
		maxStanzas, isSet := muc.History.MaxStanzas.Get()
		if !isSet || maxStanzas != 0 {
			t.Errorf("sent[%d] History.MaxStanzas = %d, isSet = %v, want 0, true", i, maxStanzas, isSet)
		}
	}
}

func TestSendGroup_ErrorsWhenNotConnected(t *testing.T) {
	t.Parallel()

	c := New(Config{}, testLogger(), observability.NewMetrics(prometheus.NewRegistry()))
	if err := c.SendGroup("sala@conference.example", "oi"); err == nil {
		t.Error("SendGroup() error = nil, want error when not connected")
	}
}

func TestJoinRooms_FirstCallDoesNotCountAsReconnect(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics(prometheus.NewRegistry())
	c := &client{
		cfg:     Config{Nickname: "tradutor"},
		logger:  testLogger(),
		metrics: metrics,
	}

	c.joinRooms(&fakeSender{})

	if got := testutil.ToFloat64(metrics.XMPPReconnectsTotal); got != 0 {
		t.Errorf("XMPPReconnectsTotal = %v, want 0 na primeira conexão", got)
	}
}

func TestJoinRooms_SubsequentCallsCountAsReconnect(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics(prometheus.NewRegistry())
	c := &client{
		cfg:     Config{Nickname: "tradutor"},
		logger:  testLogger(),
		metrics: metrics,
	}

	c.joinRooms(&fakeSender{}) // conexão inicial
	c.joinRooms(&fakeSender{}) // reconexão 1
	c.joinRooms(&fakeSender{}) // reconexão 2

	if got := testutil.ToFloat64(metrics.XMPPReconnectsTotal); got != 2 {
		t.Errorf("XMPPReconnectsTotal = %v, want 2", got)
	}
}

func TestHandleMessage_DeliversNormalizedMessageToIncoming(t *testing.T) {
	t.Parallel()

	c := &client{
		cfg:      Config{Nickname: "tradutor"},
		logger:   testLogger(),
		incoming: make(chan IncomingMessage, 1),
	}

	c.handleMessage(nil, stanza.Message{
		Attrs: stanza.Attrs{Type: stanza.MessageTypeGroupchat, From: "sala@conference.example/alice"},
		Body:  "hello",
	})

	select {
	case im := <-c.Incoming():
		if im.FromNick != "alice" || im.Body != "hello" {
			t.Errorf("got %+v", im)
		}
	default:
		t.Fatal("no message delivered to Incoming()")
	}
}

func TestHandleMessage_IgnoresNonMessagePackets(t *testing.T) {
	t.Parallel()

	c := &client{
		cfg:      Config{Nickname: "tradutor"},
		logger:   testLogger(),
		incoming: make(chan IncomingMessage, 1),
	}

	c.handleMessage(nil, stanza.Presence{})

	select {
	case im := <-c.Incoming():
		t.Fatalf("unexpected message delivered: %+v", im)
	default:
	}
}
