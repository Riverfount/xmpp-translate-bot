package pipeline_test

import (
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/pipeline"
)

func TestParseCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           string
		wantKind     pipeline.Kind
		wantArgs     string
		wantExplicit bool
	}{
		{"sem prefixo é tradução", "Hello world", pipeline.KindTranslate, "Hello world", false},
		{"tr explícito", "!tr Hello world", pipeline.KindTranslate, "Hello world", true},
		{"tr protege texto com bang", "!tr !wiki isso é literal", pipeline.KindTranslate, "!wiki isso é literal", true},
		{"wiki", "!wiki kubernetes", pipeline.KindWiki, "kubernetes", true},
		{"def é alias de wiki", "!def kubernetes", pipeline.KindWiki, "kubernetes", true},
		{"emoji", "!emoji foguete", pipeline.KindEmoji, "foguete", true},
		{"case-insensitive", "!WiKi Golang", pipeline.KindWiki, "Golang", true},
		{"verbo desconhecido vira tradução do texto inteiro", "!xyz abc", pipeline.KindTranslate, "!xyz abc", false},
		{"bang solto não é comando", "!!! wow", pipeline.KindTranslate, "!!! wow", false},
		{"verbo sem argumento", "!wiki", pipeline.KindWiki, "", true},
		{"espaços nas pontas", "   !emoji   gato   ", pipeline.KindEmoji, "gato", true},
		{"vazio", "", pipeline.KindTranslate, "", false},
		{"prefixo de verbo não casa", "!wikipedia coisa", pipeline.KindTranslate, "!wikipedia coisa", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pipeline.ParseCommand(tt.in)
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.Args != tt.wantArgs {
				t.Errorf("Args = %q, want %q", got.Args, tt.wantArgs)
			}
			if got.Explicit != tt.wantExplicit {
				t.Errorf("Explicit = %v, want %v", got.Explicit, tt.wantExplicit)
			}
		})
	}
}

func TestKind_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind pipeline.Kind
		want string
	}{
		{pipeline.KindTranslate, "translate"},
		{pipeline.KindWiki, "wiki"},
		{pipeline.KindEmoji, "emoji"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("Kind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
