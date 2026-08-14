package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Riverfount/xmpp-translate-bot/internal/config"
	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

// Mensagens de erro fixas — as únicas cujo texto não depende de parâmetros
// da tradução em si.
const (
	MsgServiceUnavailable  = "Serviço de tradução indisponível. Tente novamente."
	MsgTimeout             = "Tempo de tradução excedido. Tente novamente."
	MsgUnsupportedLanguage = "Idioma não suportado."
	MsgAuthError           = "Erro de autenticação no serviço de tradução."
)

// Formatter monta todas as mensagens de saída do bot e resolve o idioma de
// destino de uma tradução — fonte única de string, pra manter a UX
// consistente e centralizar a política de pares de idioma.
type Formatter struct {
	Nickname      string
	DefaultTarget string
	Pairs         []config.LanguagePair
	// MaxTextLength limita o tamanho (em caracteres) do texto aceito pra
	// tradução. Zero desativa o limite.
	MaxTextLength int
}

// ResolveTarget resolve o destino pra um texto detectado como lang: o
// primeiro par explícito lang:X que casar (nessa ordem), senão
// DefaultTarget. Quando o resultado é igual a lang, o texto já está no
// idioma destino e não deve ser traduzido.
func (f Formatter) ResolveTarget(lang string) string {
	for _, p := range f.Pairs {
		if p.Source == lang {
			return p.Target
		}
	}
	return f.DefaultTarget
}

// Success formata uma tradução bem-sucedida.
func (f Formatter) Success(source, target, text string) string {
	return fmt.Sprintf("Tradução (%s → %s): %s", source, target, text)
}

// Help formata a mensagem de ajuda pra menção sem texto.
func (f Formatter) Help() string {
	return fmt.Sprintf("Use: @%s [sua mensagem]. Ex: @%s Hello world", f.Nickname, f.Nickname)
}

// AlreadyTarget formata o aviso de que o texto já está no idioma destino.
func (f Formatter) AlreadyTarget(target string) string {
	return fmt.Sprintf("A mensagem já está em %s.", target)
}

// TextTooLong formata o aviso de texto acima de MaxTextLength.
func (f Formatter) TextTooLong() string {
	return fmt.Sprintf("Texto muito longo (máximo %d caracteres).", f.MaxTextLength)
}

// TranslateError mapeia um erro do Detector/Translator pra uma mensagem de
// usuário, conforme a tabela de erros HTTP → comportamento: 400 é idioma não
// suportado, 401/403 é falha de autenticação, deadline do job estourado é
// timeout, e qualquer outra coisa (429/5xx esgotados, erro de rede) cai em
// "serviço indisponível".
func (f Formatter) TranslateError(err error) string {
	var httpErr *translate.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return MsgAuthError
		case http.StatusBadRequest:
			return MsgUnsupportedLanguage
		default:
			return MsgServiceUnavailable
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return MsgTimeout
	}
	return MsgServiceUnavailable
}
