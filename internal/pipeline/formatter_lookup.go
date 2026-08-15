package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Riverfount/xmpp-translate-bot/internal/lookup"
)

// Mensagens fixas dos comandos de consulta.
const (
	MsgLookupUnavailable = "Fonte de consulta indisponível. Tente novamente."
	MsgLookupTimeout     = "Tempo de consulta excedido. Tente novamente."
	MsgLookupRateLimited = "Muitas consultas seguidas. Aguarde um instante."
)

// HelpFor devolve a ajuda do comando kind — usada quando o usuário manda o
// verbo sem argumento ("@bot !wiki").
func (f Formatter) HelpFor(k Kind) string {
	switch k {
	case KindWiki:
		return fmt.Sprintf("Use: @%s !wiki [termo]. Ex: @%s !wiki Kubernetes", f.Nickname, f.Nickname)
	case KindEmoji:
		return fmt.Sprintf("Use: @%s !emoji [palavra]. Ex: @%s !emoji foguete", f.Nickname, f.Nickname)
	default:
		return f.Help()
	}
}

// LookupSuccess formata um Result pra sala. O link vai sempre junto: no caso
// das wikis é exigência da licença CC BY-SA, e no do emoji é a referência da
// Emojipedia.
func (f Formatter) LookupSuccess(k Kind, r lookup.Result) string {
	if k == KindEmoji {
		return fmt.Sprintf("%s — %s", r.Body, r.URL)
	}
	return fmt.Sprintf("%s: %s — %s", r.Title, r.Body, r.URL)
}

// LookupNotFound formata a resposta de termo não encontrado, com sugestões
// quando a fonte ofereceu alguma.
func (f Formatter) LookupNotFound(k Kind, query string, suggestions []string) string {
	what := "Nada encontrado"
	if k == KindEmoji {
		what = "Nenhum emoji encontrado"
	}

	msg := fmt.Sprintf("%s para %q.", what, query)
	if len(suggestions) > 0 {
		msg += " " + f.Suggestions(suggestions)
	}
	return msg
}

// Suggestions formata a lista de alternativas próximas.
func (f Formatter) Suggestions(suggestions []string) string {
	return "Você quis dizer: " + strings.Join(suggestions, ", ") + "?"
}

// CommandDisabled avisa que o comando existe mas não foi habilitado na
// config — melhor que silêncio, que o usuário leria como bot travado.
func (f Formatter) CommandDisabled(k Kind) string {
	return fmt.Sprintf("O comando !%s não está habilitado neste bot.", k)
}

// QueryTooLong avisa que o termo de busca passou do limite.
func (f Formatter) QueryTooLong(max int) string {
	return fmt.Sprintf("Termo de busca muito longo (máximo %d caracteres).", max)
}

// LookupError mapeia um erro de fonte pra mensagem de usuário, seguindo a
// mesma política de TranslateError: 429 tem mensagem própria (é acionável
// pelo usuário — basta esperar), deadline vira timeout, o resto vira
// indisponibilidade genérica.
func (f Formatter) LookupError(err error) string {
	var httpErr *lookup.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == http.StatusTooManyRequests {
			return MsgLookupRateLimited
		}
		return MsgLookupUnavailable
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return MsgLookupTimeout
	}
	return MsgLookupUnavailable
}
