package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
	"unicode/utf8"

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
	// done é fechado por Start quando todos os workers já saíram — único
	// lugar que chama wg.Wait(); Stop só observa done, nunca chama Wait()
	// ele mesmo (chamar Wait() de dois lugares concorrentes é o clássico
	// mau uso de sync.WaitGroup: um Add() que chega depois de um Wait() já
	// ter retornado é indefinido, e o -race pega isso).
	done chan struct{}

	// mu protege stopped: Submit toma RLock (permite múltiplos submits
	// concorrentes), Stop toma Lock exclusivo antes de fechar jobs, o que
	// evita o clássico "send on closed channel" entre as duas.
	mu      sync.RWMutex
	stopped bool
}

// NewDispatcher cria um Dispatcher com fila bufferizada em cfg.QueueSize.
// Start precisa ser chamado pra começar a processar os jobs.
func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	return &Dispatcher{
		cfg:  cfg,
		jobs: make(chan TranslationJob, cfg.QueueSize),
		done: make(chan struct{}),
	}
}

// Start sobe cfg.Workers goroutines consumindo a fila e bloqueia até todas
// saírem — o que acontece quando ctx é cancelado (parada imediata, abandona
// o que sobrar na fila) ou quando Stop fecha a fila (parada graciosa, com
// deadline de drain). ctx não afeta o contexto de um job individual — cada
// job só é limitado pelo seu próprio JobTimeout, justamente pra sobreviver a
// um shutdown gracioso em andamento.
func (d *Dispatcher) Start(ctx context.Context) {
	for i := 0; i < d.cfg.Workers; i++ {
		d.wg.Add(1)
		go d.worker(ctx)
	}
	d.wg.Wait()
	close(d.done)
}

// Submit enfileira job pra processamento. Nunca bloqueia: se a fila estiver
// cheia, descarta o job e loga queue_full em vez de atrasar a leitura de
// novas mensagens do XMPP. Depois de Stop, novos jobs são rejeitados.
func (d *Dispatcher) Submit(job TranslationJob) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.stopped {
		d.cfg.Logger.Warn("job_rejected_shutting_down", "room", job.Room)
		return
	}

	select {
	case d.jobs <- job:
	default:
		d.cfg.Logger.Warn("queue_full", "room", job.Room)
		d.cfg.Metrics.QueueDroppedTotal.Inc()
	}
}

// Stop para de aceitar novos jobs (Submit passa a rejeitar) e fecha a fila,
// deixando os workers ativos drenarem o que já foi enfileirado. Bloqueia até
// a fila esvaziar ou drainTimeout esgotar, o que vier primeiro — depois do
// deadline, workers ainda ocupados são abandonados (a chamada retorna, mas
// eles continuam rodando até terminar ou o processo sair). Chamar Stop mais
// de uma vez não tem efeito depois da primeira.
func (d *Dispatcher) Stop(drainTimeout time.Duration) {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	close(d.jobs)
	d.mu.Unlock()

	select {
	case <-d.done:
	case <-time.After(drainTimeout):
		d.cfg.Logger.Warn("shutdown_drain_timeout", "timeout", drainTimeout.String())
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
			d.runJob(ctx, job)
		}
	}
}

// runJob processa job isoladamente: um panic em qualquer etapa (Detector,
// Translator, Formatter ou Responder são todos plugáveis via config) é
// recuperado aqui e nunca derruba o worker nem o processo — falha em um job
// nunca afeta outros.
func (d *Dispatcher) runJob(ctx context.Context, job TranslationJob) {
	d.cfg.Metrics.WorkerPoolActive.Inc()
	defer d.cfg.Metrics.WorkerPoolActive.Dec()

	defer func() {
		if r := recover(); r != nil {
			d.cfg.Logger.Error("job_panic",
				"room", job.Room,
				"panic", fmt.Sprint(r),
				"stack", string(debug.Stack()),
			)
		}
	}()

	d.process(ctx, job)
}

func (d *Dispatcher) process(ctx context.Context, job TranslationJob) {
	start := time.Now()

	if job.Text == "" {
		d.respond(job.Room, d.cfg.Formatter.Help())
		return
	}

	if max := d.cfg.Formatter.MaxTextLength; max > 0 && utf8.RuneCountInString(job.Text) > max {
		d.cfg.Logger.Info("text_too_long", "room", job.Room, "length", utf8.RuneCountInString(job.Text))
		d.respond(job.Room, d.cfg.Formatter.TextTooLong())
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
