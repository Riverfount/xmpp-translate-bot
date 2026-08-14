package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

// Responder envia a resposta formatada de volta pra sala. Satisfeito por
// xmpp.Client.SendGroup.
type Responder interface {
	SendGroup(room, body string) error
}

// DispatcherConfig reúne as dependências do worker pool.
type DispatcherConfig struct {
	Workers    int
	QueueSize  int
	Detector   translate.Detector
	Translator translate.Translator
	Responder  Responder
	Formatter  Formatter
	JobTimeout time.Duration
	Logger     *slog.Logger
	Metrics    *observability.Metrics
	// Influx é opcional — nil quando o writer do InfluxDB2 está desabilitado.
	Influx *observability.InfluxWriter
}

// Dispatcher é o worker pool que processa TranslationJob de ponta a ponta:
// detectar → validar → traduzir → formatar → responder.
type Dispatcher struct {
	cfg  DispatcherConfig
	jobs chan TranslationJob
	wg   sync.WaitGroup
}

// NewDispatcher cria um Dispatcher com fila bufferizada em cfg.QueueSize.
// Start precisa ser chamado pra começar a processar os jobs.
func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	return &Dispatcher{
		cfg:  cfg,
		jobs: make(chan TranslationJob, cfg.QueueSize),
	}
}

// Start sobe cfg.Workers goroutines consumindo a fila e bloqueia até ctx ser
// cancelado, quando os workers em andamento terminam o job atual e saem —
// jobs ainda na fila são abandonados (drenar dentro de um deadline é escopo
// de uma fase futura de resiliência).
func (d *Dispatcher) Start(ctx context.Context) {
	for i := 0; i < d.cfg.Workers; i++ {
		d.wg.Add(1)
		go d.worker(ctx)
	}
	d.wg.Wait()
}

// Submit enfileira job pra processamento. Nunca bloqueia: se a fila estiver
// cheia, descarta o job e loga queue_full em vez de atrasar a leitura de
// novas mensagens do XMPP.
func (d *Dispatcher) Submit(job TranslationJob) {
	select {
	case d.jobs <- job:
	default:
		d.cfg.Logger.Warn("queue_full", "room", job.Room)
		d.cfg.Metrics.QueueDroppedTotal.Inc()
	}
}

func (d *Dispatcher) worker(ctx context.Context) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-d.jobs:
			if !ok {
				return
			}
			d.cfg.Metrics.WorkerPoolActive.Inc()
			d.process(ctx, job)
			d.cfg.Metrics.WorkerPoolActive.Dec()
		}
	}
}

func (d *Dispatcher) process(ctx context.Context, job TranslationJob) {
	start := time.Now()

	if job.Text == "" {
		d.respond(job.Room, d.cfg.Formatter.Help())
		return
	}

	jobCtx, cancel := context.WithTimeout(ctx, d.cfg.JobTimeout)
	defer cancel()

	lang, confidence, err := d.cfg.Detector.Detect(jobCtx, job.Text)
	if err != nil {
		d.recordOutcome(job, start, "", "", 0, "error", err)
		d.respond(job.Room, d.cfg.Formatter.TranslateError(err))
		return
	}

	target := d.cfg.Formatter.ResolveTarget(lang)
	if lang == target {
		d.recordOutcome(job, start, lang, target, confidence, "already_target", nil)
		d.respond(job.Room, d.cfg.Formatter.AlreadyTarget(target))
		return
	}

	translated, err := d.cfg.Translator.Translate(jobCtx, job.Text, lang, target)
	if err != nil {
		d.recordOutcome(job, start, lang, target, confidence, "error", err)
		d.respond(job.Room, d.cfg.Formatter.TranslateError(err))
		return
	}

	d.recordOutcome(job, start, lang, target, confidence, "success", nil)
	d.respond(job.Room, d.cfg.Formatter.Success(lang, target, translated))
}

func (d *Dispatcher) respond(room, body string) {
	if err := d.cfg.Responder.SendGroup(room, body); err != nil {
		d.cfg.Logger.Error("send_failed", "room", room, "error", err.Error())
	}
}

// recordOutcome é o único ponto de fan-out de observabilidade por job: log
// estruturado, métricas Prometheus e o evento assíncrono pro InfluxDB — os
// três sinks partem do mesmo cálculo de latência/status, sem duplicação.
// Nunca loga o texto da mensagem (privacidade).
func (d *Dispatcher) recordOutcome(job TranslationJob, start time.Time, sourceLang, targetLang string, confidence float64, status string, err error) {
	duration := time.Since(start)

	logAttrs := []any{
		"room", job.Room,
		"detected_lang", sourceLang,
		"target_lang", targetLang,
		"status", status,
		"confidence", confidence,
		"latency_ms", duration.Milliseconds(),
	}
	if err != nil {
		logAttrs = append(logAttrs, "error", err.Error())
		d.cfg.Metrics.LibreTranslateErrorsTotal.WithLabelValues(errorKind(err)).Inc()
	}
	d.cfg.Logger.Info("translation_completed", logAttrs...)

	d.cfg.Metrics.TranslationsTotal.WithLabelValues(status, sourceLang, targetLang).Inc()
	d.cfg.Metrics.TranslationLatencySeconds.WithLabelValues(status).Observe(duration.Seconds())

	if d.cfg.Influx != nil {
		d.cfg.Influx.Enqueue(observability.TranslationEvent{
			SrcLang:  sourceLang,
			DstLang:  targetLang,
			MUC:      job.Room,
			Status:   status,
			Duration: duration,
		})
	}
}

// errorKind classifica err pro label "kind" de libretranslate_errors_total.
func errorKind(err error) string {
	var httpErr *translate.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden {
			return "auth"
		}
		return "http"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "network"
}

// JobTimeout deriva o timeout total de um job a partir do timeout de uma
// chamada ao LibreTranslate e do número de retries configurado — cobre o
// pior caso de uma operação (detect OU translate) esgotando todas as
// tentativas. As duas chamadas de um job compartilham esse mesmo deadline:
// se detect precisar de retry, translate herda o que sobrar do orçamento.
func JobTimeout(ltTimeout time.Duration, maxRetries int) time.Duration {
	return ltTimeout * time.Duration(1+maxRetries)
}
