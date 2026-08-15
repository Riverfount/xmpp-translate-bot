# Integração dos comandos `!tr` / `!wiki` / `!emoji`

Arquivos **novos** (copiar direto):

```
internal/pipeline/command.go
internal/pipeline/command_test.go
internal/pipeline/formatter_lookup.go
internal/pipeline/worker_lookup.go
internal/pipeline/worker_lookup_test.go
internal/lookup/lookup.go
internal/lookup/httpjson.go
internal/lookup/mediawiki.go
internal/lookup/mediawiki_test.go
internal/lookup/emoji.go
internal/lookup/emoji_test.go
internal/lookup/data/emoji_seed.json
```

Arquivos **existentes** a editar: são 5, todos com diff pequeno.

---

## 1. `internal/pipeline/worker.go`

### 1.1 Remover o método `process`

`worker_lookup.go` traz a nova `process` (que roteia por comando) e a
`processTranslate` (o corpo antigo, intacto, só recebendo o texto já sem o
verbo). **Apague a `func (d *Dispatcher) process(...)` atual de worker.go** —
se ficarem as duas, o build quebra com método duplicado.

`recordOutcome`, `respond`, `errorKind`, `runJob` e todo o resto ficam onde
estão e continuam sendo usados.

### 1.2 Adicionar as fontes na `DispatcherConfig`

```go
 type DispatcherConfig struct {
 	Workers    int
 	QueueSize  int
 	Detector   translate.Detector
 	Translator translate.Translator
+	// Wiki e Emoji são opcionais: nil desabilita o comando, e o bot
+	// responde dizendo isso em vez de ficar mudo.
+	Wiki       lookup.Looker
+	Emoji      lookup.Looker
 	Responder  Responder
 	Formatter  Formatter
 	JobTimeout time.Duration
```

Mais o import:

```go
+	"github.com/Riverfount/xmpp-translate-bot/internal/lookup"
```

Depois disso, `unicode/utf8` provavelmente fica sem uso em `worker.go`
(migrou pra `worker_lookup.go`) — o `go vet` acusa; é só remover o import.

---

## 2. `internal/observability/metrics.go`

Duas métricas novas. Não reaproveitei `translations_total` de propósito: as
labels `src_lang`/`dst_lang` não fazem sentido pra consulta e sujariam os
dashboards existentes.

```go
+	LookupsTotal *prometheus.CounterVec
+	LookupLatencySeconds *prometheus.HistogramVec
```

E no construtor, junto dos outros registros:

```go
+	m.LookupsTotal = promauto.With(reg).NewCounterVec(
+		prometheus.CounterOpts{
+			Name: "lookups_total",
+			Help: "Consultas por comando e resultado.",
+		},
+		[]string{"command", "status"},
+	)
+	m.LookupLatencySeconds = promauto.With(reg).NewHistogramVec(
+		prometheus.HistogramOpts{
+			Name:    "lookup_latency_seconds",
+			Help:    "Latência de consultas por comando e resultado.",
+			Buckets: prometheus.DefBuckets,
+		},
+		[]string{"command", "status"},
+	)
```

Ajuste o estilo (`promauto.With(reg)` vs `reg.MustRegister`) pro que o
arquivo já usa. `status` assume `success`, `not_found` ou `error`.

---

## 3. `internal/config/config.go`

### 3.1 Structs

```go
+// WikiConfig configura o comando !wiki. Host pode apontar pra qualquer wiki
+// MediaWiki com REST API v1: pt.wikipedia.org, en.wiktionary.org,
+// wiki.archlinux.org etc.
+type WikiConfig struct {
+	Enabled         bool   `yaml:"enabled"`
+	Host            string `yaml:"host"`
+	Lang            string `yaml:"lang"`
+	UserAgent       string `yaml:"user_agent"`
+	MaxExtractChars int    `yaml:"max_extract_chars"`
+	TimeoutMs       int    `yaml:"timeout_ms"`
+	MaxRetries      int    `yaml:"max_retries"`
+}
+
+// EmojiConfig configura o comando !emoji. DataFile vazio usa o dataset
+// embutido no binário.
+type EmojiConfig struct {
+	Enabled    bool   `yaml:"enabled"`
+	DataFile   string `yaml:"data_file"`
+	MaxResults int    `yaml:"max_results"`
+	Lang       string `yaml:"lang"`
+}
```

```go
 type Config struct {
 	...
 	Influx         InfluxConfig         `yaml:"influx"`
+	Wiki           WikiConfig           `yaml:"wiki"`
+	Emoji          EmojiConfig          `yaml:"emoji"`
 }
```

### 3.2 Defaults

```go
+		Wiki: WikiConfig{
+			Enabled:         true,
+			Host:            "pt.wikipedia.org",
+			Lang:            "pt",
+			MaxExtractChars: 600,
+			TimeoutMs:       5000,
+			MaxRetries:      2,
+		},
+		Emoji: EmojiConfig{
+			Enabled:    true,
+			MaxResults: 8,
+			Lang:       "pt",
+		},
```

### 3.3 Env em `applyEnv`

```go
+	if v, ok := os.LookupEnv("WIKI_ENABLED"); ok {
+		b, err := strconv.ParseBool(v)
+		if err != nil {
+			return fmt.Errorf("config: WIKI_ENABLED inválido: %w", err)
+		}
+		cfg.Wiki.Enabled = b
+	}
+	if v, ok := os.LookupEnv("WIKI_HOST"); ok {
+		cfg.Wiki.Host = v
+	}
+	if v, ok := os.LookupEnv("WIKI_LANG"); ok {
+		cfg.Wiki.Lang = v
+	}
+	if v, ok := os.LookupEnv("WIKI_USER_AGENT"); ok {
+		cfg.Wiki.UserAgent = v
+	}
+	if v, ok := os.LookupEnv("WIKI_MAX_EXTRACT_CHARS"); ok {
+		n, err := strconv.Atoi(v)
+		if err != nil {
+			return fmt.Errorf("config: WIKI_MAX_EXTRACT_CHARS inválido: %w", err)
+		}
+		cfg.Wiki.MaxExtractChars = n
+	}
+	if v, ok := os.LookupEnv("WIKI_TIMEOUT_MS"); ok {
+		n, err := strconv.Atoi(v)
+		if err != nil {
+			return fmt.Errorf("config: WIKI_TIMEOUT_MS inválido: %w", err)
+		}
+		cfg.Wiki.TimeoutMs = n
+	}
+	if v, ok := os.LookupEnv("WIKI_MAX_RETRIES"); ok {
+		n, err := strconv.Atoi(v)
+		if err != nil {
+			return fmt.Errorf("config: WIKI_MAX_RETRIES inválido: %w", err)
+		}
+		cfg.Wiki.MaxRetries = n
+	}
+
+	if v, ok := os.LookupEnv("EMOJI_ENABLED"); ok {
+		b, err := strconv.ParseBool(v)
+		if err != nil {
+			return fmt.Errorf("config: EMOJI_ENABLED inválido: %w", err)
+		}
+		cfg.Emoji.Enabled = b
+	}
+	if v, ok := os.LookupEnv("EMOJI_DATA_FILE"); ok {
+		cfg.Emoji.DataFile = v
+	}
+	if v, ok := os.LookupEnv("EMOJI_MAX_RESULTS"); ok {
+		n, err := strconv.Atoi(v)
+		if err != nil {
+			return fmt.Errorf("config: EMOJI_MAX_RESULTS inválido: %w", err)
+		}
+		cfg.Emoji.MaxResults = n
+	}
```

### 3.4 Validação em `validate` — a parte que importa

O `User-Agent` **não** é opcional: a política da Wikimedia manda identificar o
bot com uma forma de contato, e requisição sem isso leva **403**. Falhar no
boot é muito melhor que descobrir em produção.

```go
+	if cfg.Wiki.Enabled {
+		if cfg.Wiki.Host == "" {
+			return errors.New("config: WIKI_HOST é obrigatório com WIKI_ENABLED=true")
+		}
+		// https://meta.wikimedia.org/wiki/User-Agent_policy
+		if cfg.Wiki.UserAgent == "" {
+			return errors.New("config: WIKI_USER_AGENT é obrigatório — a Wikimedia responde 403 sem User-Agent descritivo")
+		}
+	}
```

---

## 4. `cmd/bot/main.go`

Depois de criar `metrics`/`logger` e antes do `NewDispatcher`:

```go
+	var wikiLooker lookup.Looker
+	if cfg.Wiki.Enabled {
+		wikiLooker = lookup.NewMediaWiki(lookup.MediaWikiConfig{
+			Host:            cfg.Wiki.Host,
+			Lang:            cfg.Wiki.Lang,
+			UserAgent:       cfg.Wiki.UserAgent,
+			MaxExtractChars: cfg.Wiki.MaxExtractChars,
+			Timeout:         time.Duration(cfg.Wiki.TimeoutMs) * time.Millisecond,
+			MaxRetries:      cfg.Wiki.MaxRetries,
+		})
+	}
+
+	// O dataset de emoji é carregado no boot: se o arquivo apontado estiver
+	// quebrado, o processo não sobe — mesma política fail-fast da config.
+	var emojiLooker lookup.Looker
+	if cfg.Emoji.Enabled {
+		store, err := lookup.NewEmojiStore(lookup.EmojiConfig{
+			DataFile:   cfg.Emoji.DataFile,
+			MaxResults: cfg.Emoji.MaxResults,
+			Lang:       cfg.Emoji.Lang,
+		})
+		if err != nil {
+			return fmt.Errorf("carregando dataset de emoji: %w", err)
+		}
+		emojiLooker = store
+	}
```

E no `DispatcherConfig`:

```go
 	dispatcher := pipeline.NewDispatcher(pipeline.DispatcherConfig{
 		...
+		Wiki:  wikiLooker,
+		Emoji: emojiLooker,
 		...
 	})
```

`dispatchIncoming` **não muda**: o parsing de comando acontece dentro de
`process`, então nem `xmpp.ParseMention` nem `TranslationJob` foram tocados.
Isso é de propósito — mantém a camada de transporte ignorante do domínio e o
diff pequeno.

### Sobre o `JobTimeout`

Hoje ele vem de `pipeline.JobTimeout(ltTimeout, maxRetries)`, derivado do
LibreTranslate. As consultas usam o mesmo deadline, o que é aceitável
(`WIKI_TIMEOUT_MS` já limita cada requisição individual). Se `WIKI_TIMEOUT_MS`
ficar maior que o timeout do LT, considere passar o maior dos dois.

---

## 5. `configs/config.example.yaml` e `.env.example`

```yaml
wiki:
  enabled: true
  host: pt.wikipedia.org
  lang: pt
  # Obrigatório. A Wikimedia exige identificação com contato.
  user_agent: "xmpp-translate-bot/1.0 (https://exemplo.org; admin@exemplo.org)"
  max_extract_chars: 600
  timeout_ms: 5000
  max_retries: 2

emoji:
  enabled: true
  # Vazio usa o dataset embutido (~70 emojis). Ver docs/emoji.md pra
  # baixar o CLDR completo.
  data_file: ""
  max_results: 8
  lang: pt
```

```
WIKI_ENABLED=true
WIKI_HOST=pt.wikipedia.org
WIKI_USER_AGENT=xmpp-translate-bot/1.0 (https://exemplo.org; admin@exemplo.org)
EMOJI_ENABLED=true
EMOJI_DATA_FILE=
```

---

## Ordem de verificação

```
go build ./...      # pega o process duplicado, se sobrou
go vet ./...        # pega os imports órfãos em worker.go
go test ./...
```

Testes de `internal/lookup` não tocam a rede (httptest + dataset embutido),
então rodam em CI isolado normalmente.

---

## Sobre o dataset de emoji (`docs/emoji.md`)

O embutido cobre o dia a dia de uma sala. Pra cobertura completa (~3.700
emojis, com todos os sinônimos em pt-BR), baixe o CLDR do Unicode:

```
curl -fsSL -o /etc/xmpp-translate-bot/emoji-pt.json \
  https://raw.githubusercontent.com/unicode-org/cldr-json/main/cldr-json/cldr-annotations-full/annotations/pt/annotations.json
```

e aponte `EMOJI_DATA_FILE` pra ele. O formato é idêntico ao do seed, então o
parser é o mesmo. Trocando `pt` por `en`, `es` etc. no path você muda o idioma
das buscas. Vale versionar esse arquivo junto com a imagem do container em vez
de baixar no boot — evita dependência de rede na inicialização.