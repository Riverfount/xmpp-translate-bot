package observability

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TranslationEvent é o evento de uma tradução (sucesso ou erro) enviado ao
// InfluxDB2 — sink de observabilidade adicional ao Prometheus, não o substitui.
type TranslationEvent struct {
	SrcLang  string
	DstLang  string
	MUC      string
	Status   string
	Duration time.Duration
}

// InfluxWriterConfig configura o InfluxWriter. Decoupled de internal/config
// deliberadamente — quem monta o writer (cmd/bot) mapeia config.InfluxConfig
// para isto.
type InfluxWriterConfig struct {
	URL       string
	Org       string
	Bucket    string
	Token     string
	Timeout   time.Duration
	QueueSize int
}

// InfluxWriter envia TranslationEvent de forma assíncrona e best-effort para
// um endpoint InfluxDB2, em InfluxDB line protocol. Nunca bloqueia o chamador
// de Enqueue: se a fila interna estiver cheia, o evento é descartado e
// contabilizado em InfluxWriteErrorsTotal. Sem retry — falha de rede ou HTTP
// só é logada e contabilizada.
type InfluxWriter struct {
	cfg      InfluxWriterConfig
	logger   *slog.Logger
	metrics  *Metrics
	client   *http.Client
	writeURL string
	queue    chan TranslationEvent
	done     chan struct{}
}

// NewInfluxWriter cria o writer e já inicia a goroutine de drain da fila.
func NewInfluxWriter(cfg InfluxWriterConfig, logger *slog.Logger, metrics *Metrics) *InfluxWriter {
	w := &InfluxWriter{
		cfg:      cfg,
		logger:   logger,
		metrics:  metrics,
		client:   &http.Client{Timeout: cfg.Timeout},
		writeURL: buildWriteURL(cfg),
		queue:    make(chan TranslationEvent, cfg.QueueSize),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

func buildWriteURL(cfg InfluxWriterConfig) string {
	q := url.Values{
		"org":       {cfg.Org},
		"bucket":    {cfg.Bucket},
		"precision": {"s"},
	}
	return strings.TrimRight(cfg.URL, "/") + "/api/v2/write?" + q.Encode()
}

// Enqueue agenda evt para envio assíncrono. Nunca bloqueia: se a fila
// interna estiver cheia, descarta o evento, loga e incrementa
// InfluxWriteErrorsTotal.
func (w *InfluxWriter) Enqueue(evt TranslationEvent) {
	select {
	case w.queue <- evt:
	default:
		w.logger.Warn("influx_queue_full", "src_lang", evt.SrcLang, "dst_lang", evt.DstLang, "muc", evt.MUC)
		w.metrics.InfluxWriteErrorsTotal.Inc()
	}
}

// Close para de aceitar eventos novos e bloqueia até a fila esvaziar.
func (w *InfluxWriter) Close() {
	close(w.queue)
	<-w.done
}

func (w *InfluxWriter) run() {
	defer close(w.done)
	for evt := range w.queue {
		w.send(evt)
	}
}

func (w *InfluxWriter) send(evt TranslationEvent) {
	line := lineProtocol(evt)
	req, err := http.NewRequest(http.MethodPost, w.writeURL, strings.NewReader(line))
	if err != nil {
		w.logger.Warn("influx_write_failed", "error", err.Error())
		w.metrics.InfluxWriteErrorsTotal.Inc()
		return
	}
	req.Header.Set("Authorization", "Token "+w.cfg.Token)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := w.client.Do(req)
	if err != nil {
		w.logger.Warn("influx_write_failed", "error", err.Error())
		w.metrics.InfluxWriteErrorsTotal.Inc()
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		w.logger.Warn("influx_write_failed", "status", resp.StatusCode)
		w.metrics.InfluxWriteErrorsTotal.Inc()
	}
}

var tagEscaper = strings.NewReplacer(
	`\`, `\\`,
	",", `\,`,
	"=", `\=`,
	" ", `\ `,
)

func escapeTag(s string) string {
	return tagEscaper.Replace(s)
}

// lineProtocol monta a line protocol do InfluxDB para evt, no formato
// `transbot,src_lang=<>,dst_lang=<>,muc=<>,status=<> duration=<segundos>`.
func lineProtocol(evt TranslationEvent) string {
	tags := fmt.Sprintf("src_lang=%s,dst_lang=%s,muc=%s,status=%s",
		escapeTag(evt.SrcLang), escapeTag(evt.DstLang), escapeTag(evt.MUC), escapeTag(evt.Status))
	duration := strconv.FormatFloat(evt.Duration.Seconds(), 'f', -1, 64)
	return "transbot," + tags + " duration=" + duration
}
