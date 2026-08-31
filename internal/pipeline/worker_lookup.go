package pipeline

import (
	"context"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/Riverfount/xmpp-translate-bot/internal/lookup"
)

// maxLookupQueryRunes limita o termo de busca de !wiki e !emoji. Não usa o
// MaxTextLength da tradução de propósito: 5000 caracteres fazem sentido pra
// um texto a traduzir, nenhum pra um verbete.
const maxLookupQueryRunes = 100

// process roteia um job pelo comando que a mensagem carrega. Substitui a
// process() original de worker.go — o corpo antigo virou processTranslate.
func (d *Dispatcher) process(ctx context.Context, job TranslationJob) {
	if job.Text == "" {
		d.respond(job.Room, d.cfg.Formatter.Help())
		return
	}

	cmd := ParseCommand(job.Text)
	if cmd.Args == "" {
		d.respond(job.Room, d.cfg.Formatter.HelpFor(cmd.Kind))
		return
	}

	switch cmd.Kind {
	case KindWiki, KindEmoji:
		d.processLookup(ctx, job, cmd.Kind, cmd.Args)
	default:
		d.processTranslate(ctx, job, cmd.Args)
	}
}

// processTranslate é o fluxo original de tradução: detectar → validar →
// traduzir → formatar → responder. text já vem sem o verbo do comando, então
// "!tr Hello" traduz "Hello" e não a linha inteira.
func (d *Dispatcher) processTranslate(ctx context.Context, job TranslationJob, text string) {
	start := time.Now()

	if max := d.cfg.Formatter.MaxTextLength; max > 0 && utf8.RuneCountInString(text) > max {
		d.cfg.Logger.Info("text_too_long", "room", job.Room, "length", utf8.RuneCountInString(text))
		d.respond(job.Room, d.cfg.Formatter.TextTooLong())
		return
	}

	jobCtx, cancel := context.WithTimeout(ctx, d.cfg.JobTimeout)
	defer cancel()

	lang, confidence, err := d.cfg.Detector.Detect(jobCtx, text)
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

	translated, err := d.cfg.Translator.Translate(jobCtx, text, lang, target)
	if err != nil {
		d.recordOutcome(job, start, lang, target, confidence, "error", err)
		d.respond(job.Room, d.cfg.Formatter.TranslateError(err))
		return
	}

	d.recordOutcome(job, start, lang, target, confidence, "success", nil)
	d.respond(job.Room, d.cfg.Formatter.Success(lang, target, translated))
}

// processLookup atende !wiki e !emoji. Os dois compartilham o mesmo fluxo
// porque a diferença está toda atrás da interface Looker — o emoji só não
// chega a fazer I/O.
func (d *Dispatcher) processLookup(ctx context.Context, job TranslationJob, kind Kind, query string) {
	start := time.Now()

	looker := d.lookerFor(kind)
	if looker == nil {
		d.respond(job.Room, d.cfg.Formatter.CommandDisabled(kind))
		return
	}

	if utf8.RuneCountInString(query) > maxLookupQueryRunes {
		d.respond(job.Room, d.cfg.Formatter.QueryTooLong(maxLookupQueryRunes))
		return
	}

	jobCtx, cancel := context.WithTimeout(ctx, d.cfg.JobTimeout)
	defer cancel()

	res, err := looker.Look(jobCtx, query)

	var notFound *lookup.NotFoundError
	switch {
	case errors.As(err, &notFound) && res.Body == "":
		d.recordLookup(job, kind, start, "not_found", res.Source, err)
		d.respond(job.Room, d.cfg.Formatter.LookupNotFound(kind, query, notFound.Suggestions))
		return
	case err != nil && !errors.As(err, &notFound):
		d.recordLookup(job, kind, start, "error", res.Source, err)
		d.respond(job.Room, d.cfg.Formatter.LookupError(err))
		return
	}

	// Desambiguação: veio conteúdo E um NotFoundError com alternativas. Vale
	// responder o que veio e listar as outras opções.
	d.recordLookup(job, kind, start, "success", res.Source, nil)
	body := d.cfg.Formatter.LookupSuccess(kind, res)
	if notFound != nil && len(notFound.Suggestions) > 0 {
		body += " " + d.cfg.Formatter.Suggestions(notFound.Suggestions)
	}
	d.respond(job.Room, body)
}

// lookerFor devolve a fonte do comando, ou nil quando ele não foi habilitado
// na config — nesse caso o bot responde que o comando está desligado em vez
// de ficar mudo.
func (d *Dispatcher) lookerFor(kind Kind) lookup.Looker {
	switch kind {
	case KindWiki:
		if d.cfg.Wiki == nil {
			return nil
		}
		return d.cfg.Wiki
	case KindEmoji:
		if d.cfg.Emoji == nil {
			return nil
		}
		return d.cfg.Emoji
	default:
		return nil
	}
}

// recordLookup é o equivalente de recordOutcome para consultas: log
// estruturado + métricas, sem nunca logar o termo pesquisado (privacidade,
// mesma regra da tradução).
func (d *Dispatcher) recordLookup(job TranslationJob, kind Kind, start time.Time, status, source string, err error) {
	duration := time.Since(start)

	attrs := []any{
		"room", job.Room,
		"command", kind.String(),
		"source", source,
		"status", status,
		"latency_ms", duration.Milliseconds(),
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error(), "error_kind", lookupErrorKind(err))
	}
	d.cfg.Logger.Info("lookup_completed", attrs...)

	d.cfg.Metrics.LookupsTotal.WithLabelValues(kind.String(), status).Inc()
	d.cfg.Metrics.LookupLatencySeconds.WithLabelValues(kind.String(), status).Observe(duration.Seconds())
}

// lookupErrorKind classifica err pro label de erro, no mesmo espírito de
// errorKind() da tradução.
func lookupErrorKind(err error) string {
	var httpErr *lookup.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == http.StatusTooManyRequests {
			return "rate_limited"
		}
		return "http"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "network"
}
