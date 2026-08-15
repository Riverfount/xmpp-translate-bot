package lookup_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/lookup"
)

func newTestEmojiStore(t *testing.T) *lookup.EmojiStore {
	t.Helper()
	s, err := lookup.NewEmojiStore(lookup.EmojiConfig{MaxResults: 5, Lang: "pt"})
	if err != nil {
		t.Fatalf("NewEmojiStore() error = %v", err)
	}
	return s
}

func TestEmojiStore_Look_ExactNameWins(t *testing.T) {
	t.Parallel()

	got, err := newTestEmojiStore(t).Look(context.Background(), "foguete")
	if err != nil {
		t.Fatalf("Look() error = %v", err)
	}
	if !strings.HasPrefix(got.Body, "🚀") {
		t.Errorf("Body = %q, want 🚀 primeiro", got.Body)
	}
	if got.URL == "" {
		t.Error("URL vazia, want link de referência")
	}
}

func TestEmojiStore_Look_MatchesKeyword(t *testing.T) {
	t.Parallel()

	got, err := newTestEmojiStore(t).Look(context.Background(), "docker")
	if err != nil {
		t.Fatalf("Look() error = %v", err)
	}
	if !strings.Contains(got.Body, "🐳") {
		t.Errorf("Body = %q, want 🐳 (sinônimo 'docker')", got.Body)
	}
}

func TestEmojiStore_Look_IgnoresCaseAndAccents(t *testing.T) {
	t.Parallel()

	s := newTestEmojiStore(t)
	for _, q := range []string{"CORAÇÃO", "coracao", "Coração"} {
		got, err := s.Look(context.Background(), q)
		if err != nil {
			t.Fatalf("Look(%q) error = %v", q, err)
		}
		if !strings.Contains(got.Body, "❤️") {
			t.Errorf("Look(%q) body = %q, want ❤️", q, got.Body)
		}
	}
}

func TestEmojiStore_Look_RespectsMaxResults(t *testing.T) {
	t.Parallel()

	s, err := lookup.NewEmojiStore(lookup.EmojiConfig{MaxResults: 2})
	if err != nil {
		t.Fatalf("NewEmojiStore() error = %v", err)
	}

	got, err := s.Look(context.Background(), "rosto")
	if err != nil {
		t.Fatalf("Look() error = %v", err)
	}
	if n := strings.Count(got.Body, "·") + 1; n > 2 {
		t.Errorf("Body tem %d resultados, want <= 2: %q", n, got.Body)
	}
}

func TestEmojiStore_Look_NotFound(t *testing.T) {
	t.Parallel()

	_, err := newTestEmojiStore(t).Look(context.Background(), "zzzzqqqq")
	var nf *lookup.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("Look() error = %v, want *NotFoundError", err)
	}
}

func TestEmojiStore_Look_EmptyQuery(t *testing.T) {
	t.Parallel()

	_, err := newTestEmojiStore(t).Look(context.Background(), "   ")
	if err == nil {
		t.Fatal("Look(\"\") error = nil, want erro")
	}
}

func TestNewEmojiStore_MissingDataFile(t *testing.T) {
	t.Parallel()

	_, err := lookup.NewEmojiStore(lookup.EmojiConfig{DataFile: "/nao/existe/annotations.json"})
	if err == nil {
		t.Fatal("NewEmojiStore() error = nil, want falha no boot (fail-fast)")
	}
}
