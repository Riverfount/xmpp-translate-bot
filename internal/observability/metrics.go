package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics agrupa as métricas Prometheus do bot, incluindo o writer InfluxDB2.
type Metrics struct {
	TranslationsTotal         *prometheus.CounterVec
	TranslationLatencySeconds *prometheus.HistogramVec
	LibreTranslateErrorsTotal *prometheus.CounterVec
	XMPPReconnectsTotal       prometheus.Counter
	QueueDroppedTotal         prometheus.Counter
	WorkerPoolActive          prometheus.Gauge
	InfluxWriteErrorsTotal    prometheus.Counter
}

// NewMetrics cria e registra todas as métricas do bot em reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		TranslationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "translations_total",
			Help: "Total de traduções processadas, por status e par de idiomas.",
		}, []string{"status", "source_lang", "target_lang"}),

		TranslationLatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "translation_latency_seconds",
			Help:    "Latência do fluxo de tradução completo, por status.",
			Buckets: prometheus.DefBuckets,
		}, []string{"status"}),

		LibreTranslateErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "libretranslate_errors_total",
			Help: "Total de erros ao chamar o LibreTranslate, por tipo.",
		}, []string{"kind"}),

		XMPPReconnectsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "xmpp_reconnects_total",
			Help: "Total de reconexões ao servidor XMPP.",
		}),

		QueueDroppedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_dropped_total",
			Help: "Total de jobs descartados por fila cheia (backpressure).",
		}),

		WorkerPoolActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worker_pool_active",
			Help: "Número de workers ativos processando um job no momento.",
		}),

		InfluxWriteErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "influx_write_errors_total",
			Help: "Total de falhas ao escrever eventos no InfluxDB2.",
		}),
	}

	reg.MustRegister(
		m.TranslationsTotal,
		m.TranslationLatencySeconds,
		m.LibreTranslateErrorsTotal,
		m.XMPPReconnectsTotal,
		m.QueueDroppedTotal,
		m.WorkerPoolActive,
		m.InfluxWriteErrorsTotal,
	)

	return m
}
