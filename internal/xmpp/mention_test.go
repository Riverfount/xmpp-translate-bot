package xmpp

import "testing"

func TestParseMention_AtPrefixWithText(t *testing.T) {
	t.Parallel()

	got := ParseMention("tradutor", "@tradutor Hello world")
	want := ParseResult{Mentioned: true, Text: "Hello world"}
	if got != want {
		t.Errorf("ParseMention() = %+v, want %+v", got, want)
	}
}

func TestParseMention_ColonPrefixWithText(t *testing.T) {
	t.Parallel()

	got := ParseMention("tradutor", "tradutor: Hello world")
	want := ParseResult{Mentioned: true, Text: "Hello world"}
	if got != want {
		t.Errorf("ParseMention() = %+v, want %+v", got, want)
	}
}

func TestParseMention_WithoutTextSignalsHelp(t *testing.T) {
	t.Parallel()

	tests := []string{
		"@tradutor",
		"@tradutor   ",
		"tradutor:",
		"tradutor:   ",
	}
	for _, body := range tests {
		got := ParseMention("tradutor", body)
		if !got.Mentioned || got.Text != "" {
			t.Errorf("ParseMention(%q) = %+v, want Mentioned=true, Text=\"\"", body, got)
		}
	}
}

func TestParseMention_MidSentenceDoesNotTrigger(t *testing.T) {
	t.Parallel()

	got := ParseMention("tradutor", "oi @tradutor tudo bem?")
	if got.Mentioned {
		t.Errorf("ParseMention() = %+v, want Mentioned=false", got)
	}
}

func TestParseMention_CaseInsensitive(t *testing.T) {
	t.Parallel()

	tests := []string{
		"@TRADUTOR hello",
		"@Tradutor hello",
		"TRADUTOR: hello",
		"Tradutor: hello",
	}
	for _, body := range tests {
		got := ParseMention("tradutor", body)
		want := ParseResult{Mentioned: true, Text: "hello"}
		if got != want {
			t.Errorf("ParseMention(%q) = %+v, want %+v", body, got, want)
		}
	}
}

func TestParseMention_MultipleSpacesAfterPrefixAreTrimmed(t *testing.T) {
	t.Parallel()

	got := ParseMention("tradutor", "@tradutor     hello   world")
	want := ParseResult{Mentioned: true, Text: "hello   world"}
	if got != want {
		t.Errorf("ParseMention() = %+v, want %+v", got, want)
	}
}

func TestParseMention_LongerNicknameSharingPrefixDoesNotMatch(t *testing.T) {
	t.Parallel()

	got := ParseMention("tradutor", "@tradutorzinho hello")
	if got.Mentioned {
		t.Errorf("ParseMention() = %+v, want Mentioned=false", got)
	}
}

func TestParseMention_NoMentionAtAll(t *testing.T) {
	t.Parallel()

	got := ParseMention("tradutor", "hello world")
	if got.Mentioned {
		t.Errorf("ParseMention() = %+v, want Mentioned=false", got)
	}
}

func TestParseMention_TrimsLeadingWhitespaceBeforeMention(t *testing.T) {
	t.Parallel()

	got := ParseMention("tradutor", "   @tradutor hello")
	want := ParseResult{Mentioned: true, Text: "hello"}
	if got != want {
		t.Errorf("ParseMention() = %+v, want %+v", got, want)
	}
}
