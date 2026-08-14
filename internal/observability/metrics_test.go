package observability_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

func TestNewMetrics_TranslationsTotal(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics(prometheus.NewRegistry())

	m.TranslationsTotal.WithLabelValues("success", "en", "pt").Inc()

	got := testutil.ToFloat64(m.TranslationsTotal.WithLabelValues("success", "en", "pt"))
	if got != 1 {
		t.Errorf("TranslationsTotal = %v, want 1", got)
	}
}

func TestNewMetrics_TranslationLatencySeconds(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics(prometheus.NewRegistry())

	m.TranslationLatencySeconds.WithLabelValues("success").Observe(0.842)

	if n := testutil.CollectAndCount(m.TranslationLatencySeconds); n != 1 {
		t.Errorf("CollectAndCount(TranslationLatencySeconds) = %d, want 1", n)
	}
}

func TestNewMetrics_LibreTranslateErrorsTotal(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics(prometheus.NewRegistry())

	m.LibreTranslateErrorsTotal.WithLabelValues("timeout").Inc()

	got := testutil.ToFloat64(m.LibreTranslateErrorsTotal.WithLabelValues("timeout"))
	if got != 1 {
		t.Errorf("LibreTranslateErrorsTotal = %v, want 1", got)
	}
}

func TestNewMetrics_XMPPReconnectsTotal(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics(prometheus.NewRegistry())

	m.XMPPReconnectsTotal.Inc()
	m.XMPPReconnectsTotal.Inc()

	if got := testutil.ToFloat64(m.XMPPReconnectsTotal); got != 2 {
		t.Errorf("XMPPReconnectsTotal = %v, want 2", got)
	}
}

func TestNewMetrics_QueueDroppedTotal(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics(prometheus.NewRegistry())

	m.QueueDroppedTotal.Inc()

	if got := testutil.ToFloat64(m.QueueDroppedTotal); got != 1 {
		t.Errorf("QueueDroppedTotal = %v, want 1", got)
	}
}

func TestNewMetrics_WorkerPoolActive(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics(prometheus.NewRegistry())

	m.WorkerPoolActive.Set(5)

	if got := testutil.ToFloat64(m.WorkerPoolActive); got != 5 {
		t.Errorf("WorkerPoolActive = %v, want 5", got)
	}
}

func TestNewMetrics_InfluxWriteErrorsTotal(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics(prometheus.NewRegistry())

	m.InfluxWriteErrorsTotal.Inc()

	if got := testutil.ToFloat64(m.InfluxWriteErrorsTotal); got != 1 {
		t.Errorf("InfluxWriteErrorsTotal = %v, want 1", got)
	}
}

func TestNewMetrics_IndependentRegistries(t *testing.T) {
	t.Parallel()
	// NewMetrics deve poder ser chamado mais de uma vez (ex: em testes)
	// desde que cada chamada use um Registerer diferente, sem panicar por
	// registro duplicado.
	_ = observability.NewMetrics(prometheus.NewRegistry())
	_ = observability.NewMetrics(prometheus.NewRegistry())
}
