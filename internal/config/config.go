// Package config carrega e valida a configuração do bot: YAML opcional +
// override por variável de ambiente (env sempre vence), com validação
// fail-fast no boot.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type XMPPConfig struct {
	JID      string   `yaml:"jid"`
	Password string   `yaml:"-"`
	Server   string   `yaml:"server"`
	TLS      bool     `yaml:"tls"`
	Rooms    []string `yaml:"rooms"`
	Nickname string   `yaml:"nickname"`
}

type LibreTranslateConfig struct {
	URL        string `yaml:"url"`
	APIKey     string `yaml:"-"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	MaxRetries int    `yaml:"max_retries"`
}

// LanguagePair é um par origem:destino de TRANSLATION_PAIRS / translation.pairs.
type LanguagePair struct {
	Source string
	Target string
}

type TranslationConfig struct {
	DefaultTarget string         `yaml:"default_target"`
	Pairs         []LanguagePair `yaml:"-"`
	Detector      string         `yaml:"detector"`
	MaxTextLength int            `yaml:"max_text_length"`
}

type PipelineConfig struct {
	Workers int `yaml:"workers"`
	Queue   int `yaml:"queue"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type MetricsConfig struct {
	Addr string `yaml:"addr"`
}

// InfluxConfig configura o writer assíncrono de métricas para InfluxDB2.
// Desabilitado por padrão: só exige URL/Org/Bucket/Token quando Enabled=true.
type InfluxConfig struct {
	Enabled   bool   `yaml:"enabled"`
	URL       string `yaml:"url"`
	Org       string `yaml:"org"`
	Bucket    string `yaml:"bucket"`
	Token     string `yaml:"-"`
	TimeoutMs int    `yaml:"timeout_ms"`
	QueueSize int    `yaml:"queue_size"`
}

type Config struct {
	XMPP           XMPPConfig           `yaml:"xmpp"`
	LibreTranslate LibreTranslateConfig `yaml:"libretranslate"`
	Translation    TranslationConfig    `yaml:"translation"`
	Pipeline       PipelineConfig       `yaml:"pipeline"`
	Logging        LoggingConfig        `yaml:"logging"`
	Metrics        MetricsConfig        `yaml:"metrics"`
	Influx         InfluxConfig         `yaml:"influx"`
}

func defaults() *Config {
	return &Config{
		XMPP: XMPPConfig{
			TLS:      true,
			Nickname: "tradutor",
		},
		LibreTranslate: LibreTranslateConfig{
			TimeoutMs:  5000,
			MaxRetries: 2,
		},
		Translation: TranslationConfig{
			DefaultTarget: "pt",
			Detector:      "libretranslate",
			MaxTextLength: 5000,
		},
		Pipeline: PipelineConfig{
			Workers: 10,
			Queue:   100,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
		Metrics: MetricsConfig{
			Addr: ":9090",
		},
		Influx: InfluxConfig{
			Enabled:   false,
			TimeoutMs: 5000,
			QueueSize: 100,
		},
	}
}

// Load monta a config a partir de defaults + YAML opcional (yamlPath, ignorado
// se vazio) + override por env (env sempre vence), e valida o resultado.
func Load(yamlPath string) (*Config, error) {
	cfg := defaults()

	if yamlPath != "" {
		if err := applyYAML(cfg, yamlPath); err != nil {
			return nil, err
		}
	}

	if err := applyEnv(cfg); err != nil {
		return nil, err
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyYAML(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: lendo yaml %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config: parseando yaml %q: %w", path, err)
	}
	// translation.pairs no YAML vem como lista de strings "src:dst"; o campo
	// Pairs (yaml:"-") é preenchido separadamente a partir dela.
	var raw struct {
		Translation struct {
			Pairs []string `yaml:"pairs"`
		} `yaml:"translation"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("config: parseando yaml %q: %w", path, err)
	}
	if len(raw.Translation.Pairs) > 0 {
		pairs, err := parsePairs(strings.Join(raw.Translation.Pairs, ","))
		if err != nil {
			return fmt.Errorf("config: yaml %q: %w", path, err)
		}
		cfg.Translation.Pairs = pairs
	}
	return nil
}

func applyEnv(cfg *Config) error {
	if v, ok := os.LookupEnv("XMPP_JID"); ok {
		cfg.XMPP.JID = v
	}
	if v, ok := os.LookupEnv("XMPP_PASSWORD"); ok {
		cfg.XMPP.Password = v
	}
	if v, ok := os.LookupEnv("XMPP_SERVER"); ok {
		cfg.XMPP.Server = v
	}
	if v, ok := os.LookupEnv("XMPP_TLS"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("config: XMPP_TLS inválido: %w", err)
		}
		cfg.XMPP.TLS = b
	}
	if v, ok := os.LookupEnv("XMPP_ROOMS"); ok {
		cfg.XMPP.Rooms = splitNonEmpty(v)
	}
	if v, ok := os.LookupEnv("XMPP_NICKNAME"); ok {
		cfg.XMPP.Nickname = v
	}

	if v, ok := os.LookupEnv("LT_URL"); ok {
		cfg.LibreTranslate.URL = v
	}
	if v, ok := os.LookupEnv("LT_API_KEY"); ok {
		cfg.LibreTranslate.APIKey = v
	}
	if v, ok := os.LookupEnv("LT_TIMEOUT_MS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: LT_TIMEOUT_MS inválido: %w", err)
		}
		cfg.LibreTranslate.TimeoutMs = n
	}
	if v, ok := os.LookupEnv("LT_MAX_RETRIES"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: LT_MAX_RETRIES inválido: %w", err)
		}
		cfg.LibreTranslate.MaxRetries = n
	}

	if v, ok := os.LookupEnv("DEFAULT_TARGET"); ok {
		cfg.Translation.DefaultTarget = v
	}
	if v, ok := os.LookupEnv("TRANSLATION_PAIRS"); ok {
		pairs, err := parsePairs(v)
		if err != nil {
			return fmt.Errorf("config: TRANSLATION_PAIRS: %w", err)
		}
		cfg.Translation.Pairs = pairs
	}
	if v, ok := os.LookupEnv("DETECTOR"); ok {
		cfg.Translation.Detector = v
	}
	if v, ok := os.LookupEnv("MAX_TEXT_LENGTH"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: MAX_TEXT_LENGTH inválido: %w", err)
		}
		cfg.Translation.MaxTextLength = n
	}

	if v, ok := os.LookupEnv("WORKER_POOL_SIZE"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: WORKER_POOL_SIZE inválido: %w", err)
		}
		cfg.Pipeline.Workers = n
	}
	if v, ok := os.LookupEnv("QUEUE_SIZE"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: QUEUE_SIZE inválido: %w", err)
		}
		cfg.Pipeline.Queue = n
	}

	if v, ok := os.LookupEnv("LOG_LEVEL"); ok {
		cfg.Logging.Level = v
	}
	if v, ok := os.LookupEnv("METRICS_ADDR"); ok {
		cfg.Metrics.Addr = v
	}

	if v, ok := os.LookupEnv("INFLUX_ENABLED"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("config: INFLUX_ENABLED inválido: %w", err)
		}
		cfg.Influx.Enabled = b
	}
	if v, ok := os.LookupEnv("INFLUX_URL"); ok {
		cfg.Influx.URL = v
	}
	if v, ok := os.LookupEnv("INFLUX_ORG"); ok {
		cfg.Influx.Org = v
	}
	if v, ok := os.LookupEnv("INFLUX_BUCKET"); ok {
		cfg.Influx.Bucket = v
	}
	if v, ok := os.LookupEnv("INFLUX_TOKEN"); ok {
		cfg.Influx.Token = v
	}
	if v, ok := os.LookupEnv("INFLUX_TIMEOUT_MS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: INFLUX_TIMEOUT_MS inválido: %w", err)
		}
		cfg.Influx.TimeoutMs = n
	}
	if v, ok := os.LookupEnv("INFLUX_QUEUE_SIZE"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: INFLUX_QUEUE_SIZE inválido: %w", err)
		}
		cfg.Influx.QueueSize = n
	}

	return nil
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parsePairs(raw string) ([]LanguagePair, error) {
	var pairs []LanguagePair
	for _, p := range splitNonEmpty(raw) {
		src, dst, ok := strings.Cut(p, ":")
		src, dst = strings.TrimSpace(src), strings.TrimSpace(dst)
		if !ok || src == "" || dst == "" {
			return nil, fmt.Errorf("par de idioma inválido %q, esperado formato origem:destino", p)
		}
		pairs = append(pairs, LanguagePair{Source: src, Target: dst})
	}
	return pairs, nil
}

func validate(cfg *Config) error {
	var errs []error

	if cfg.XMPP.JID == "" {
		errs = append(errs, errors.New("XMPP_JID é obrigatório"))
	}
	if cfg.XMPP.Password == "" {
		errs = append(errs, errors.New("XMPP_PASSWORD é obrigatório"))
	}
	if cfg.XMPP.Server == "" {
		errs = append(errs, errors.New("XMPP_SERVER é obrigatório"))
	}
	if len(cfg.XMPP.Rooms) == 0 {
		errs = append(errs, errors.New("XMPP_ROOMS é obrigatório (ao menos uma sala)"))
	}

	if cfg.LibreTranslate.URL == "" {
		errs = append(errs, errors.New("LT_URL é obrigatório"))
	} else if u, err := url.Parse(cfg.LibreTranslate.URL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("LT_URL malformada: %q", cfg.LibreTranslate.URL))
	}
	if cfg.LibreTranslate.APIKey == "" {
		errs = append(errs, errors.New("LT_API_KEY é obrigatório"))
	}

	if cfg.Translation.DefaultTarget == "" {
		errs = append(errs, errors.New("DEFAULT_TARGET não pode ser vazio"))
	}

	if cfg.Influx.Enabled {
		if cfg.Influx.URL == "" {
			errs = append(errs, errors.New("INFLUX_URL é obrigatório quando INFLUX_ENABLED=true"))
		}
		if cfg.Influx.Org == "" {
			errs = append(errs, errors.New("INFLUX_ORG é obrigatório quando INFLUX_ENABLED=true"))
		}
		if cfg.Influx.Bucket == "" {
			errs = append(errs, errors.New("INFLUX_BUCKET é obrigatório quando INFLUX_ENABLED=true"))
		}
		if cfg.Influx.Token == "" {
			errs = append(errs, errors.New("INFLUX_TOKEN é obrigatório quando INFLUX_ENABLED=true"))
		}
	}

	return errors.Join(errs...)
}
