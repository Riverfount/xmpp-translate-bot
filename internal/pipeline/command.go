package pipeline

import (
	"strings"
	"unicode"
)

// CommandPrefix é o sigil que marca um comando explícito. Sem ele — ou com
// um verbo desconhecido depois dele — a mensagem inteira é tratada como
// texto pra traduzir, que é o comportamento default e retrocompatível.
const CommandPrefix = "!"

// Kind identifica qual comando a menção pediu.
type Kind int

const (
	// KindTranslate é o default: qualquer coisa que não seja um comando
	// explícito conhecido cai aqui.
	KindTranslate Kind = iota
	KindWiki
	KindEmoji
)

// String devolve o nome do comando, usado como label de métrica e em logs.
func (k Kind) String() string {
	switch k {
	case KindWiki:
		return "wiki"
	case KindEmoji:
		return "emoji"
	default:
		return "translate"
	}
}

// commandAliases mapeia verbo (sem o prefixo, minúsculo) para Kind. "tr"
// existe mesmo sendo o default: é o escape hatch pra traduzir um texto que
// por acaso começa com "!" ou com um verbo reservado.
var commandAliases = map[string]Kind{
	"tr":        KindTranslate,
	"traduz":    KindTranslate,
	"traduzir":  KindTranslate,
	"translate": KindTranslate,

	"wiki":      KindWiki,
	"def":       KindWiki,
	"define":    KindWiki,
	"definicao": KindWiki,
	"definição": KindWiki,

	"emoji":  KindEmoji,
	"emojis": KindEmoji,
	"e":      KindEmoji,
}

// Command é uma menção já resolvida em verbo + argumento.
type Command struct {
	Kind Kind
	// Args é o texto depois do verbo, sem espaços nas pontas. Vazio
	// sinaliza o fluxo de ajuda do comando.
	Args string
	// Explicit reporta se o usuário escreveu o comando (!wiki) ou se caiu
	// no default implícito. Só interessa pra observabilidade e pra decidir
	// qual ajuda mostrar.
	Explicit bool
}

// ParseCommand resolve text em um Command. As regras, nessa ordem:
//
//	"!wiki golang"   -> KindWiki,      Args="golang",       Explicit
//	"!emoji foguete" -> KindEmoji,     Args="foguete",      Explicit
//	"!tr !wiki isso" -> KindTranslate, Args="!wiki isso",   Explicit
//	"Hello world"    -> KindTranslate, Args="Hello world"
//	"!xyz abc"       -> KindTranslate, Args="!xyz abc"      (verbo desconhecido)
//
// Verbo desconhecido cai no default em vez de virar erro: a mensagem pode
// ser legitimamente um texto que começa com "!", e traduzir é sempre a
// resposta menos surpreendente.
func ParseCommand(text string) Command {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, CommandPrefix) {
		return Command{Kind: KindTranslate, Args: trimmed}
	}

	verb, rest := cutVerb(strings.TrimPrefix(trimmed, CommandPrefix))
	kind, ok := commandAliases[strings.ToLower(verb)]
	if !ok {
		return Command{Kind: KindTranslate, Args: trimmed}
	}
	return Command{Kind: kind, Args: strings.TrimSpace(rest), Explicit: true}
}

// cutVerb separa a primeira palavra de s do restante, cortando em qualquer
// espaço Unicode (não só ASCII 0x20).
func cutVerb(s string) (verb, rest string) {
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}
