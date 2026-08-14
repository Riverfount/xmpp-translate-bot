package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/config"
	"github.com/Riverfount/xmpp-translate-bot/internal/pipeline"
	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

func TestFormatter_ResolveTarget_ExplicitPairWins(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{
		DefaultTarget: "pt",
		Pairs: []config.LanguagePair{
			{Source: "en", Target: "es"},
		},
	}

	if got := f.ResolveTarget("en"); got != "es" {
		t.Errorf("ResolveTarget(en) = %q, want %q", got, "es")
	}
}

func TestFormatter_ResolveTarget_FirstMatchWins(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{
		DefaultTarget: "pt",
		Pairs: []config.LanguagePair{
			{Source: "en", Target: "es"},
			{Source: "en", Target: "fr"},
		},
	}

	if got := f.ResolveTarget("en"); got != "es" {
		t.Errorf("ResolveTarget(en) = %q, want %q (primeiro match)", got, "es")
	}
}

func TestFormatter_ResolveTarget_FallsBackToDefault(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{
		DefaultTarget: "pt",
		Pairs: []config.LanguagePair{
			{Source: "en", Target: "es"},
		},
	}

	if got := f.ResolveTarget("fr"); got != "pt" {
		t.Errorf("ResolveTarget(fr) = %q, want %q (default)", got, "pt")
	}
}

func TestFormatter_ResolveTarget_BR005_SourceEqualsDefault(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{DefaultTarget: "pt"}

	got := f.ResolveTarget("pt")
	if got != "pt" {
		t.Errorf("ResolveTarget(pt) = %q, want %q", got, "pt")
	}
}

func TestFormatter_Success(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{}
	got := f.Success("en", "pt", "Olá, como você está?")
	want := "Tradução (en → pt): Olá, como você está?"
	if got != want {
		t.Errorf("Success() = %q, want %q", got, want)
	}
}

func TestFormatter_Help(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{Nickname: "tradutor"}
	got := f.Help()
	want := "Use: @tradutor [sua mensagem]. Ex: @tradutor Hello world"
	if got != want {
		t.Errorf("Help() = %q, want %q", got, want)
	}
}

func TestFormatter_AlreadyTarget(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{}
	got := f.AlreadyTarget("pt")
	want := "A mensagem já está em pt."
	if got != want {
		t.Errorf("AlreadyTarget() = %q, want %q", got, want)
	}
}

func TestFormatter_TextTooLong(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{MaxTextLength: 5000}
	got := f.TextTooLong()
	want := "Texto muito longo (máximo 5000 caracteres)."
	if got != want {
		t.Errorf("TextTooLong() = %q, want %q", got, want)
	}
}

func TestFormatter_TranslateError(t *testing.T) {
	t.Parallel()

	f := pipeline.Formatter{}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"400", &translate.HTTPError{StatusCode: 400}, pipeline.MsgUnsupportedLanguage},
		{"401", &translate.HTTPError{StatusCode: 401}, pipeline.MsgAuthError},
		{"403", &translate.HTTPError{StatusCode: 403}, pipeline.MsgAuthError},
		{"429_exhausted", &translate.HTTPError{StatusCode: 429}, pipeline.MsgServiceUnavailable},
		{"503_exhausted", &translate.HTTPError{StatusCode: 503}, pipeline.MsgServiceUnavailable},
		{"context_deadline", context.DeadlineExceeded, pipeline.MsgTimeout},
		{"network_error", errors.New("connection refused"), pipeline.MsgServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := f.TranslateError(tt.err); got != tt.want {
				t.Errorf("TranslateError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
