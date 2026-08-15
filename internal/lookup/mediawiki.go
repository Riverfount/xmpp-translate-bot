package lookup

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var _ Looker = (*MediaWiki)(nil)

// MediaWikiConfig configura uma fonte MediaWiki. Funciona pra qualquer wiki
// que rode MediaWiki com a REST API v1 habilitada — pt.wikipedia.org,
// en.wiktionary.org, wiki.archlinux.org, wikis do Fandom — trocando só o Host.
type MediaWikiConfig struct {
	// Host é o domínio da wiki, sem esquema. Ex.: "pt.wikipedia.org".
	Host string
	// Lang é o código do idioma do conteúdo, só pra rotular o Result.
	Lang string
	// UserAgent identifica o bot. Obrigatório: a Wikimedia responde 403 pra
	// User-Agent genérico. Ex.:
	// "xmpp-translate-bot/1.0 (https://exemplo.org; admin@exemplo.org)".
	UserAgent string
	// MaxExtractChars trunca o resumo antes de mandar pra sala. 0 desativa.
	MaxExtractChars int
	// MaxSuggestions limita quantas alternativas voltam num NotFoundError.
	MaxSuggestions int
	Timeout        time.Duration
	MaxRetries     int
	// Transport é opcional: nil usa o http.DefaultTransport. Existe pra
	// teste apontar o client pra um httptest.Server.
	Transport http.RoundTripper
}

// MediaWiki consulta resumos de verbetes via REST API v1.
type MediaWiki struct {
	cfg  MediaWikiConfig
	http *httpJSON
}

// NewMediaWiki cria a fonte. Não faz I/O.
func NewMediaWiki(cfg MediaWikiConfig) *MediaWiki {
	if cfg.MaxSuggestions == 0 {
		cfg.MaxSuggestions = 3
	}
	return &MediaWiki{
		cfg:  cfg,
		http: newHTTPJSON(cfg.UserAgent, cfg.Timeout, cfg.MaxRetries, cfg.Transport),
	}
}

// summaryResponse é o subconjunto de /page/summary/{title} que interessa.
type summaryResponse struct {
	// Type é "standard", "disambiguation", "no-extract"...
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Extract     string `json:"extract"`
	Lang        string `json:"lang"`
	ContentURLs struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
}

type searchResponse struct {
	Pages []struct {
		Key         string `json:"key"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"pages"`
}

// Look resolve query em um resumo de verbete. A estratégia é: tentar o título
// direto (a API já segue redirects e normaliza capitalização); se der 404 ou
// vier uma página de desambiguação, cair na busca e usar o primeiro
// resultado, devolvendo os demais como sugestões.
func (m *MediaWiki) Look(ctx context.Context, query string) (Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, &NotFoundError{Query: query}
	}

	summary, err := m.summary(ctx, query)
	switch {
	case err == nil && summary.Type != "disambiguation" && summary.Extract != "":
		return m.toResult(summary), nil
	case err != nil && !isNotFound(err):
		return Result{}, err
	}

	// Título direto não serviu: busca textual.
	hits, err := m.search(ctx, query)
	if err != nil {
		return Result{}, err
	}
	if len(hits.Pages) == 0 {
		return Result{}, &NotFoundError{Query: query}
	}

	suggestions := make([]string, 0, m.cfg.MaxSuggestions)
	for _, p := range hits.Pages[1:] {
		if len(suggestions) == m.cfg.MaxSuggestions {
			break
		}
		suggestions = append(suggestions, p.Title)
	}

	summary, err = m.summary(ctx, hits.Pages[0].Key)
	if err != nil || summary.Extract == "" {
		return Result{}, &NotFoundError{Query: query, Suggestions: append([]string{hits.Pages[0].Title}, suggestions...)}
	}

	res := m.toResult(summary)
	if summary.Type == "disambiguation" {
		return res, &NotFoundError{Query: query, Suggestions: suggestions}
	}
	return res, nil
}

func (m *MediaWiki) summary(ctx context.Context, title string) (summaryResponse, error) {
	// A REST API espera o título com "_" no lugar de espaço; PathEscape cuida
	// do resto (acentos, "/", "&").
	path := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	endpoint := "https://" + m.cfg.Host + "/api/rest_v1/page/summary/" + path + "?redirect=true"

	var out summaryResponse
	if err := m.http.get(ctx, endpoint, &out); err != nil {
		return summaryResponse{}, err
	}
	return out, nil
}

func (m *MediaWiki) search(ctx context.Context, query string) (searchResponse, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("limit", "5")
	endpoint := "https://" + m.cfg.Host + "/w/rest.php/v1/search/page?" + q.Encode()

	var out searchResponse
	if err := m.http.get(ctx, endpoint, &out); err != nil {
		return searchResponse{}, err
	}
	return out, nil
}

func (m *MediaWiki) toResult(s summaryResponse) Result {
	lang := s.Lang
	if lang == "" {
		lang = m.cfg.Lang
	}

	body := strings.TrimSpace(s.Extract)
	if s.Description != "" {
		body = s.Description + " — " + body
	}

	pageURL := s.ContentURLs.Desktop.Page
	if pageURL == "" {
		pageURL = "https://" + m.cfg.Host + "/wiki/" + url.PathEscape(strings.ReplaceAll(s.Title, " ", "_"))
	}

	return Result{
		Title:  s.Title,
		Body:   Truncate(body, m.cfg.MaxExtractChars),
		URL:    pageURL,
		Source: m.cfg.Host,
		Lang:   lang,
	}
}

// isNotFound reporta se err é um 404 da wiki — o único caso em que vale a
// pena tentar a busca textual antes de desistir.
func isNotFound(err error) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return httpErr.StatusCode == http.StatusNotFound
}
