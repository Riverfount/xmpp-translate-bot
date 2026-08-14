package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewServer monta o *http.Server que expõe /metrics (Prometheus), /healthz
// (liveness — sempre 200, o processo está de pé) e /readyz (readiness, ver
// Health). Quem chama é responsável por rodar ListenAndServe e por desligar
// o server (Shutdown) na hora certa.
func NewServer(addr string, gatherer prometheus.Gatherer, health *Health) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", health.ServeReadyz)

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}
