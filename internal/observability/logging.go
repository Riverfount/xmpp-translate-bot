// Package observability provê logging estruturado (slog JSON), métricas
// Prometheus e o writer assíncrono de eventos para InfluxDB2.
package observability

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// NewLogger cria um *slog.Logger com handler JSON escrevendo em w, filtrado
// pelo nível informado ("debug", "info", "warn" ou "error", case-insensitive).
func NewLogger(w io.Writer, level string) (*slog.Logger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler), nil
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("observability: nível de log inválido: %q", level)
	}
}
