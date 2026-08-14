package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Riverfount/xmpp-translate-bot/internal/config"
	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
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
type newXMPPClient func(xmpp.Config, *slog.Logger) xmpp.Client

func run(ctx context.Context, w io.Writer, newClient newXMPPClient) error {
	cfg, err := config.Load(os.Getenv("CONFIG_FILE"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger, err := observability.NewLogger(w, cfg.Logging.Level)
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}

	metrics := observability.NewMetrics(prometheus.NewRegistry())

	if cfg.Influx.Enabled {
		influxWriter := observability.NewInfluxWriter(observability.InfluxWriterConfig{
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

	xc := newClient(xmpp.Config{
		JID:      cfg.XMPP.JID,
		Password: cfg.XMPP.Password,
		Server:   cfg.XMPP.Server,
		TLS:      cfg.XMPP.TLS,
		Rooms:    cfg.XMPP.Rooms,
		Nickname: cfg.XMPP.Nickname,
	}, logger)

	go echoIncoming(xc, logger)

	return xc.Start(ctx)
}

// echoIncoming ecoa mensagens recebidas de volta para a sala, validando o
// caminho recepção→envio. Ignorar IsSelf é o que evita o loop de auto-eco.
func echoIncoming(xc xmpp.Client, logger *slog.Logger) {
	for msg := range xc.Incoming() {
		logger.Info("message_received", "room", msg.Room, "from", msg.FromNick, "is_self", msg.IsSelf)
		logger.Debug("message_body", "body", msg.Body)

		if msg.IsSelf || msg.Body == "" {
			continue
		}
		if err := xc.SendGroup(msg.Room, msg.Body); err != nil {
			logger.Error("echo_failed", "room", msg.Room, "error", err.Error())
		}
	}
}
