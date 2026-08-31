package lookup_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/lookup"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stringResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func newTestMediaWiki(rt http.RoundTripper) *lookup.MediaWiki {
	return lookup.NewMediaWiki(lookup.MediaWikiConfig{
		Host:      "wiki.test",
		Lang:      "pt",
		UserAgent: "test/1.0 (mailto:test@test)",
		Transport: rt,
	})
}

// Na busca de fallback, um erro transitório (5xx) no segundo summary() tem que
// ser propagado como erro — não mascarado de "não encontrado", que sujaria as
// métricas durante uma queda da wiki.
func TestMediaWiki_Look_SearchFallbackPropagatesTransientError(t *testing.T) {
	t.Parallel()

	mw := newTestMediaWiki(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Path, "/search/page"):
			return stringResponse(http.StatusOK, `{"pages":[{"key":"K8s","title":"K8s"}]}`), nil
		case strings.Contains(r.URL.Path, "/page/summary/kubernetes"):
			return stringResponse(http.StatusNotFound, `{}`), nil
		case strings.Contains(r.URL.Path, "/page/summary/K8s"):
			return stringResponse(http.StatusServiceUnavailable, `{}`), nil
		default:
			t.Errorf("requisição inesperada: %s", r.URL)
			return stringResponse(http.StatusInternalServerError, `{}`), nil
		}
	}))

	_, err := mw.Look(context.Background(), "kubernetes")

	var notFound *lookup.NotFoundError
	if errors.As(err, &notFound) {
		t.Fatalf("Look() err = %v, want erro transitório propagado, não NotFoundError", err)
	}
	var httpErr *lookup.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("Look() err = %v, want *HTTPError 503", err)
	}
}

// Já um 404 no segundo summary() continua virando NotFoundError com sugestões:
// a página aparece no índice de busca mas não tem resumo.
func TestMediaWiki_Look_SearchFallbackNotFoundStaysNotFound(t *testing.T) {
	t.Parallel()

	mw := newTestMediaWiki(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/search/page") {
			return stringResponse(http.StatusOK, `{"pages":[{"key":"K8s","title":"Kubernetes (orquestrador)"}]}`), nil
		}
		return stringResponse(http.StatusNotFound, `{}`), nil
	}))

	_, err := mw.Look(context.Background(), "kubernetes")

	var notFound *lookup.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Look() err = %v, want *NotFoundError", err)
	}
	if len(notFound.Suggestions) == 0 || notFound.Suggestions[0] != "Kubernetes (orquestrador)" {
		t.Errorf("Suggestions = %v, want o título do primeiro hit", notFound.Suggestions)
	}
}
