package lookup

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
)

var _ Looker = (*EmojiStore)(nil)

// emojiSeed é o dataset mínimo embutido, no mesmo formato do CLDR, pra que
// !emoji funcione sem nenhum arquivo externo. Pra cobertura completa (~3.7k
// emojis, com sinônimos em pt-BR), aponte EmojiConfig.DataFile para o
// annotations.json do CLDR — ver docs/emoji.md.
//
//go:embed data/emoji_seed.json
var emojiSeed []byte

// EmojiConfig configura a fonte de emoji.
type EmojiConfig struct {
	// DataFile é o caminho de um annotations.json do CLDR. Vazio usa o
	// dataset embutido.
	DataFile string
	// MaxResults limita quantos emojis entram na resposta.
	MaxResults int
	// Lang rotula o Result e nada mais — o idioma real é o do dataset.
	Lang string
}

// Emoji é uma entrada do dataset já normalizada pra busca.
type Emoji struct {
	Char     string
	Name     string
	Keywords []string

	foldedName     string
	foldedKeywords []string
}

// EmojiStore busca emojis por nome/sinônimo, inteiramente em memória: sem
// rede, sem rate limit, latência desprezível. O dataset é imutável depois de
// NewEmojiStore, então é seguro compartilhar entre os workers.
type EmojiStore struct {
	cfg    EmojiConfig
	emojis []Emoji
}

// cldrAnnotations espelha o formato do
// cldr-json/cldr-annotations-full/annotations/<lang>/annotations.json.
type cldrAnnotations struct {
	Annotations struct {
		Annotations map[string]struct {
			Default []string `json:"default"`
			TTS     []string `json:"tts"`
		} `json:"annotations"`
	} `json:"annotations"`
}

// NewEmojiStore carrega o dataset de cfg.DataFile ou, na ausência dele, do
// seed embutido. Falha no boot se o arquivo apontado não for legível ou não
// tiver nenhuma entrada — melhor não subir do que subir com !emoji mudo.
func NewEmojiStore(cfg EmojiConfig) (*EmojiStore, error) {
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 8
	}

	if cfg.DataFile == "" {

		return newEmojiStore(cfg, bytes.NewReader(emojiSeed))
	}

	f, err := os.Open(cfg.DataFile)
	if err != nil {
		return nil, fmt.Errorf("lookup: abrindo dataset de emoji %q: %w", cfg.DataFile, err)
	}
	defer func() { _ = f.Close() }()

	return newEmojiStore(cfg, f)
}

func newEmojiStore(cfg EmojiConfig, r io.Reader) (*EmojiStore, error) {
	var data cldrAnnotations
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return nil, fmt.Errorf("lookup: parseando dataset de emoji: %w", err)
	}

	entries := data.Annotations.Annotations
	if len(entries) == 0 {
		return nil, fmt.Errorf("lookup: dataset de emoji vazio")
	}

	emojis := make([]Emoji, 0, len(entries))
	for char, a := range entries {
		// O CLDR traz o nome canônico em "tts" e os sinônimos em "default".
		// Entradas sem tts são variantes de sequência; o nome vira o primeiro
		// sinônimo pra não perder a busca.
		name := ""
		if len(a.TTS) > 0 {
			name = a.TTS[0]
		} else if len(a.Default) > 0 {
			name = a.Default[0]
		} else {
			continue
		}

		e := Emoji{Char: char, Name: name, Keywords: a.Default, foldedName: fold(name)}
		e.foldedKeywords = make([]string, len(a.Default))
		for i, k := range a.Default {
			e.foldedKeywords[i] = fold(k)
		}
		emojis = append(emojis, e)
	}

	// Ordem estável: o mapa do JSON não tem ordem, e empates de score
	// precisam desempatar de forma determinística pro teste não flapar.
	sort.Slice(emojis, func(i, j int) bool { return emojis[i].Char < emojis[j].Char })

	return &EmojiStore{cfg: cfg, emojis: emojis}, nil
}

func (s *EmojiStore) Emojis() []Emoji {
	return s.emojis
}

// Count devolve quantos emojis estão carregados no dataset.
func (s *EmojiStore) Count() int {
	return len(s.emojis)
}

// Lang devolve o idioma do dataset, não do usuário.
func (s *EmojiStore) Lang() string {
	return s.cfg.Lang
}

// Look busca query no dataset e devolve os melhores emojis numa linha só.
// Não faz I/O — ctx é aceito só pra satisfazer Looker.
func (s *EmojiStore) Look(_ context.Context, query string) (Result, error) {
	q := fold(query)
	if q == "" {
		return Result{}, &NotFoundError{Query: query}
	}

	type scored struct {
		emoji Emoji
		score int
	}

	var hits []scored
	for _, e := range s.emojis {
		if sc := scoreEmoji(e, q); sc > 0 {
			hits = append(hits, scored{e, sc})
		}
	}
	if len(hits) == 0 {
		return Result{}, &NotFoundError{Query: query}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		// Empate: nome mais curto costuma ser o emoji mais "canônico"
		// ("gato" antes de "gato sorrindo com olhos sorridentes").
		return len(hits[i].emoji.Name) < len(hits[j].emoji.Name)
	})

	if len(hits) > s.cfg.MaxResults {
		hits = hits[:s.cfg.MaxResults]
	}

	parts := make([]string, len(hits))
	for i, h := range hits {
		parts[i] = h.emoji.Char + " " + h.emoji.Name
	}

	top := hits[0].emoji
	return Result{
		Title:  top.Name,
		Body:   strings.Join(parts, "  ·  "),
		URL:    emojipediaURL(top.Char),
		Source: "cldr",
		Lang:   s.cfg.Lang,
	}, nil
}

// scoreEmoji pontua o quão bem e casa com a query já normalizada. Zero
// significa nenhum casamento.
func scoreEmoji(e Emoji, q string) int {
	switch {
	case e.foldedName == q:
		return 100
	case strings.HasPrefix(e.foldedName, q):
		return 80
	case strings.Contains(e.foldedName, q):
		return 60
	}

	best := 0
	for _, k := range e.foldedKeywords {
		switch {
		case k == q && best < 50:
			best = 50
		case strings.HasPrefix(k, q) && best < 35:
			best = 35
		case strings.Contains(k, q) && best < 20:
			best = 20
		}
	}
	return best
}

// emojipediaURL monta o link de referência. A Emojipedia resolve o próprio
// caractere no path; se algum dia mudarem esse esquema, é só este ponto que
// precisa mudar.
func emojipediaURL(char string) string {
	return "https://emojipedia.org/" + url.PathEscape(char)
}

// accentFolder mapeia os acentos que aparecem em nomes de emoji em pt/es/fr
// pras letras base — evita puxar golang.org/x/text só pra isso.
var accentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "õ", "o", "ô", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// fold normaliza um termo pra comparação: minúsculo, sem acento, sem espaço
// nas pontas.
func fold(s string) string {
	return accentFolder.Replace(strings.ToLower(strings.TrimSpace(s)))
}
