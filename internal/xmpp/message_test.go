package xmpp

import (
	"encoding/xml"
	"testing"

	"gosrc.io/xmpp/stanza"
)

func TestNormalizeIncoming_GroupchatFromOther(t *testing.T) {
	t.Parallel()

	msg := stanza.Message{
		Attrs: stanza.Attrs{
			Type: stanza.MessageTypeGroupchat,
			From: "sala@conference.example/alice",
		},
		Body: "hello",
	}

	im, ok := normalizeIncoming(msg, "tradutor")
	if !ok {
		t.Fatal("normalizeIncoming() ok = false, want true")
	}
	if im.Room != "sala@conference.example" {
		t.Errorf("Room = %q", im.Room)
	}
	if im.FromNick != "alice" {
		t.Errorf("FromNick = %q", im.FromNick)
	}
	if im.Body != "hello" {
		t.Errorf("Body = %q", im.Body)
	}
	if im.IsSelf {
		t.Error("IsSelf = true, want false")
	}
	if im.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

func TestNormalizeIncoming_OwnMessageMarkedIsSelf(t *testing.T) {
	t.Parallel()

	msg := stanza.Message{
		Attrs: stanza.Attrs{
			Type: stanza.MessageTypeGroupchat,
			From: "sala@conference.example/tradutor",
		},
		Body: "echo",
	}

	im, ok := normalizeIncoming(msg, "tradutor")
	if !ok {
		t.Fatal("normalizeIncoming() ok = false, want true")
	}
	if !im.IsSelf {
		t.Error("IsSelf = false, want true")
	}
}

func TestNormalizeIncoming_IgnoresNonGroupchat(t *testing.T) {
	t.Parallel()

	for _, typ := range []stanza.StanzaType{stanza.MessageTypeChat, stanza.MessageTypeNormal, stanza.MessageTypeHeadline, ""} {
		msg := stanza.Message{
			Attrs: stanza.Attrs{Type: typ, From: "sala@conference.example/alice"},
			Body:  "hello",
		}
		if _, ok := normalizeIncoming(msg, "tradutor"); ok {
			t.Errorf("normalizeIncoming() ok = true for type %q, want false", typ)
		}
	}
}

func TestNormalizeIncoming_IgnoresHistoryReplay(t *testing.T) {
	t.Parallel()

	msg := stanza.Message{
		Attrs: stanza.Attrs{
			Type: stanza.MessageTypeGroupchat,
			From: "sala@conference.example/alice",
		},
		Body: "old message",
		Extensions: []stanza.MsgExtension{
			&Delay{From: "sala@conference.example", Stamp: "2024-01-01T00:00:00Z"},
		},
	}

	if _, ok := normalizeIncoming(msg, "tradutor"); ok {
		t.Error("normalizeIncoming() ok = true for delayed message, want false")
	}
}

func TestNormalizeIncoming_IgnoresMalformedFrom(t *testing.T) {
	t.Parallel()

	msg := stanza.Message{
		Attrs: stanza.Attrs{Type: stanza.MessageTypeGroupchat, From: ""},
		Body:  "hello",
	}

	if _, ok := normalizeIncoming(msg, "tradutor"); ok {
		t.Error("normalizeIncoming() ok = true for malformed from, want false")
	}
}

func TestDelayExtension_ParsedFromXML(t *testing.T) {
	t.Parallel()

	raw := `<message from="sala@conference.example/alice" type="groupchat">
		<body>old message</body>
		<delay xmlns="urn:xmpp:delay" from="sala@conference.example" stamp="2024-01-01T00:00:00Z"/>
	</message>`

	var msg stanza.Message
	if err := xml.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}

	var delay Delay
	if !msg.Get(&delay) {
		t.Fatal("msg.Get(&Delay{}) = false, want true")
	}
	if delay.Stamp != "2024-01-01T00:00:00Z" {
		t.Errorf("Stamp = %q", delay.Stamp)
	}
	if delay.From != "sala@conference.example" {
		t.Errorf("From = %q", delay.From)
	}
}
