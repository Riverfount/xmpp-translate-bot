// Package lookup isola as fontes de consulta do bot (wikis MediaWiki e
// dataset de emoji) atrás de uma interface única, do mesmo jeito que
// internal/translate faz com o LibreTranslate: dá pra mockar em teste e
// trocar a fonte sem tocar no pipeline.
package lookup

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Result é a resposta de uma consulta, já pronta pro Formatter.
type Result struct {
	// Title é o verbete encontrado (pode diferir da query por redirect).
	Title string
	// Body é o texto principal: o resumo do artigo ou a lista de emojis.
	Body string
	// URL é a página de origem — obrigatória na resposta por causa da
	// licença CC BY-SA do conteúdo das wikis.
	URL string
	// Source identifica a fonte ("pt.wikipedia.org", "cldr"), usado como
	// label de métrica.
	Source string
	// Lang é o idioma do conteúdo, quando aplicável.
	Lang string
}

// Looker resolve uma query em um Result.
type Looker interface {
	Look(ctx context.Context, query string) (Result, error)
}

// NotFoundError sinaliza que a query não casou com nada. Suggestions, quando
// preenchido, traz alternativas próximas pra oferecer ao usuário.
type NotFoundError struct {
	Query       string
	Suggestions []string
}

func (e *NotFoundError) Error() string {
	if len(e.Suggestions) > 0 {
		return fmt.Sprintf("lookup: %q não encontrado (sugestões: %s)", e.Query, strings.Join(e.Suggestions, ", "))
	}
	return fmt.Sprintf("lookup: %q não encontrado", e.Query)
}

// HTTPError representa uma resposta HTTP não-2xx de uma fonte remota.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("lookup: resposta %d da fonte: %s", e.StatusCode, e.Body)
}

// Truncate corta s em no máximo max runes, sem quebrar no meio de uma
// palavra quando dá, e sinaliza o corte com reticências. max <= 0 desativa.
func Truncate(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}

	runes := []rune(s)
	cut := string(runes[:max])
	// Recua até o último espaço, desde que não jogue fora mais de 20% do
	// texto — pra "…" não aparecer no meio de uma palavra. A comparação é
	// em bytes dos dois lados, de propósito.
	if i := strings.LastIndex(cut, " "); i > len(cut)*4/5 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}
