package xmpp

import (
	"strings"
	"unicode"
)

// ParseResult é o resultado de reconhecer uma menção ao bot em uma mensagem.
type ParseResult struct {
	Mentioned bool
	Text      string
}

// ParseMention reconhece uma menção ao bot no início de body (após trim),
// nas formas "@<nick>" ou "<nick>:", case-insensitive. O restante da
// mensagem, sem espaços nas pontas, vira Text — vazio sinaliza o fluxo de
// ajuda. Menções fora do início da mensagem não são reconhecidas (regra do
// MVP).
func ParseMention(nickname, body string) ParseResult {
	trimmed := strings.TrimSpace(body)

	if rest, ok := cutMentionPrefix(trimmed, "@"+nickname, true); ok {
		return ParseResult{Mentioned: true, Text: strings.TrimSpace(rest)}
	}
	if rest, ok := cutMentionPrefix(trimmed, nickname+":", false); ok {
		return ParseResult{Mentioned: true, Text: strings.TrimSpace(rest)}
	}
	return ParseResult{}
}

// cutMentionPrefix reporta se s começa com prefix (case-insensitive) e
// retorna o restante. Quando requireBoundary é true, exige espaço (ou fim de
// string) logo após o prefixo, para "@tradutorzinho" não casar com o nick
// "tradutor".
func cutMentionPrefix(s, prefix string, requireBoundary bool) (rest string, ok bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	rest = s[len(prefix):]
	if requireBoundary && rest != "" && !unicode.IsSpace(rune(rest[0])) {
		return "", false
	}
	return rest, true
}
