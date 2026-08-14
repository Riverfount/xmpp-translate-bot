package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Riverfount/xmpp-translate-bot/internal/config"
	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run carrega e valida a config, prepara logging/métricas/writer do
// InfluxDB (fundação da Fase 1) e loga o startup. w recebe os logs
// estruturados (os.Stdout em produção, um buffer em teste).
func run(w io.Writer) error {
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

	return nil
}
