package observability_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

func TestNewServer_HealthzAlwaysOK(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	srv := observability.NewServer(":0", reg, observability.NewHealth())
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", resp.StatusCode)
	}
}

func TestNewServer_ReadyzReflectsHealthState(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	health := observability.NewHealth()
	srv := observability.NewServer(":0", reg, health)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/readyz status = %d, want 503 antes de ficar pronto", resp.StatusCode)
	}

	health.SetXMPPConnected(true)
	health.SetLanguagesReady(true)

	resp2, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("/readyz status = %d, want 200 depois de ficar pronto", resp2.StatusCode)
	}
}

func TestNewServer_MetricsExposesRegisteredMetrics(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	observability.NewMetrics(reg)
	srv := observability.NewServer(":0", reg, observability.NewHealth())
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/metrics status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	// queue_dropped_total é um Counter sem labels, então aparece assim que
	// registrado — diferente de CounterVec/HistogramVec (como
	// translations_total), que só aparecem depois de alguma observação.
	if !strings.Contains(string(body), "queue_dropped_total") {
		t.Errorf("/metrics body não contém queue_dropped_total: %s", body)
	}
}
