package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Riverfount/xmpp-translate-bot/internal/config"
	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
	"github.com/Riverfount/xmpp-translate-bot/internal/pipeline"
	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
	"github.com/Riverfount/xmpp-translate-bot/internal/xmpp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Stdout, xmpp.New); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newXMPPClient permite injetar uma implementação fake de xmpp.Client nos
// testes, já que Start() faz conexão de rede real e não é barato de mockar.
type newXMPPClient func(xmpp.Config, *slog.Logger, *observability.Metrics, *observability.Health) xmpp.Client

// shutdownDrainTimeout limita quanto tempo o shutdown gracioso espera a fila
// de jobs esvaziar antes de fechar a conexão XMPP e sair mesmo assim.
const shutdownDrainTimeout = 10 * time.Second

// metricsServerShutdownTimeout limita quanto tempo o servidor HTTP de
// /metrics, /healthz e /readyz espera requisições em andamento terminarem.
const metricsServerShutdownTimeout = 5 * time.Second

func run(ctx context.Context, w io.Writer, newClient newXMPPClient) error {
	cfg, err := config.Load(os.Getenv("CONFIG_FILE"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger, err := observability.NewLogger(w, cfg.Logging.Level)
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}

	registry := prometheus.NewRegistry()
	metrics := observability.NewMetrics(registry)
	health := observability.NewHealth()

	metricsServer := observability.NewServer(cfg.Metrics.Addr, registry, health)
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics_server_failed", "error", err.Error())
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), metricsServerShutdownTimeout)
		defer cancel()
		_ = metricsServer.Shutdown(shutdownCtx)
	}()

	var influxWriter *observability.InfluxWriter
	if cfg.Influx.Enabled {
		influxWriter = observability.NewInfluxWriter(observability.InfluxWriterConfig{
			URL:       cfg.Influx.URL,
			Org:       cfg.Influx.Org,
			Bucket:    cfg.Influx.Bucket,
			Token:     cfg.Influx.Token,
			Timeout:   time.Duration(cfg.Influx.TimeoutMs) * time.Millisecond,
			QueueSize: cfg.Influx.QueueSize,
		}, logger, metrics)
		defer influxWriter.Close()
	}

	logger.Info("bot_starting",
		"rooms", cfg.XMPP.Rooms,
		"detector", cfg.Translation.Detector,
		"influx_enabled", cfg.Influx.Enabled,
	)

	ltTimeout := time.Duration(cfg.LibreTranslate.TimeoutMs) * time.Millisecond
	ltClient := translate.NewClient(cfg.LibreTranslate.URL, cfg.LibreTranslate.APIKey, ltTimeout, cfg.LibreTranslate.MaxRetries, logger)

	detector, err := translate.NewDetector(cfg.Translation.Detector, ltClient)
	if err != nil {
		return fmt.Errorf("translate: %w", err)
	}

	// Busca /languages uma vez no boot, em paralelo com a conexão XMPP (são
	// operações independentes) — só sinaliza readiness, não bloqueia o resto
	// do bootstrap nem é reagendada se falhar (retry fica pra quando alguém
	// precisar da validação de idioma de fato, não só do sinal de /readyz).
	// bgCancel aborta essa busca (em vez de deixá-la seguir seu próprio
	// timeout) e langDone garante que run() só devolve o controle depois que
	// ela parar de rodar — sem isso, a goroutine pode logar depois que o
	// chamador (ou um teste) já inspecionou a saída, uma race de verdade.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	langDone := make(chan struct{})
	go func() {
		defer close(langDone)
		langCtx, cancel := context.WithTimeout(bgCtx, pipeline.JobTimeout(ltTimeout, cfg.LibreTranslate.MaxRetries))
		defer cancel()
		if _, err := ltClient.SupportedLanguages(langCtx); err != nil {
			logger.Warn("languages_fetch_failed", "error", err.Error())
			return
		}
		health.SetLanguagesReady(true)
	}()
	defer func() {
		bgCancel()
		<-langDone
	}()

	xc := newClient(xmpp.Config{
		JID:      cfg.XMPP.JID,
		Password: cfg.XMPP.Password,
		Server:   cfg.XMPP.Server,
		TLS:      cfg.XMPP.TLS,
		Rooms:    cfg.XMPP.Rooms,
		Nickname: cfg.XMPP.Nickname,
	}, logger, metrics, health)

	dispatcher := pipeline.NewDispatcher(pipeline.DispatcherConfig{
		Workers:    cfg.Pipeline.Workers,
		QueueSize:  cfg.Pipeline.Queue,
		Detector:   detector,
		Translator: ltClient,
		Responder:  xc,
		Formatter: pipeline.Formatter{
			Nickname:      cfg.XMPP.Nickname,
			DefaultTarget: cfg.Translation.DefaultTarget,
			Pairs:         cfg.Translation.Pairs,
			MaxTextLength: cfg.Translation.MaxTextLength,
		},
		JobTimeout: pipeline.JobTimeout(ltTimeout, cfg.LibreTranslate.MaxRetries),
		Logger:     logger,
		Metrics:    metrics,
		Influx:     influxWriter,
	})

	// dispatcher.Start roda com um contexto próprio, nunca cancelado por
	// sinal: o shutdown gracioso é conduzido por dispatcher.Stop (para de
	// aceitar jobs, drena a fila dentro do deadline), não por cancelamento
	// de contexto — assim um job em andamento no momento do SIGTERM não tem
	// sua chamada ao LibreTranslate abortada no meio, só quando terminar (ou
	// estourar o próprio JobTimeout) é que o processo segue pro próximo passo.
	go dispatcher.Start(context.Background())
	go dispatchIncoming(xc, cfg.XMPP.Nickname, dispatcher)

	xmppCtx, cancelXMPP := context.WithCancel(context.Background())
	defer cancelXMPP()

	xmppErr := make(chan error, 1)
	go func() { xmppErr <- xc.Start(xmppCtx) }()

	select {
	case <-ctx.Done():
		// Shutdown gracioso: para de aceitar jobs novos e drena a fila
		// dentro do deadline antes de fechar a conexão XMPP — só assim uma
		// tradução em andamento ainda consegue responder na sala.
		dispatcher.Stop(shutdownDrainTimeout)
		cancelXMPP()
		return <-xmppErr
	case err := <-xmppErr:
		// XMPP caiu sozinho (erro fatal de conexão), sem sinal de shutdown.
		dispatcher.Stop(shutdownDrainTimeout)
		return err
	}
}

// dispatchIncoming lê as mensagens recebidas nas salas, reconhece menções ao
// bot e submete um TranslationJob por menção. Mensagens do próprio bot
// (IsSelf) nunca chegam ao parser — é o que evita o loop de autotradução.
func dispatchIncoming(xc xmpp.Client, nickname string, dispatcher *pipeline.Dispatcher) {
	for msg := range xc.Incoming() {
		if msg.IsSelf {
			continue
		}
		parsed := xmpp.ParseMention(nickname, msg.Body)
		if !parsed.Mentioned {
			continue
		}
		dispatcher.Submit(pipeline.TranslationJob{
			Room:       msg.Room,
			From:       msg.FromNick,
			Text:       parsed.Text,
			ReceivedAt: msg.Timestamp,
		})
	}
}
