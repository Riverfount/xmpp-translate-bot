package observability_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

func TestHealth_ServeReadyz_NotReadyByDefault(t *testing.T) {
	t.Parallel()

	h := observability.NewHealth()
	rec := httptest.NewRecorder()
	h.ServeReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHealth_ServeReadyz_ReadyWhenBothSignalsSet(t *testing.T) {
	t.Parallel()

	h := observability.NewHealth()
	h.SetXMPPConnected(true)
	h.SetLanguagesReady(true)

	rec := httptest.NewRecorder()
	h.ServeReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHealth_ServeReadyz_PartiallyReadyIsNotReady(t *testing.T) {
	t.Parallel()

	h := observability.NewHealth()
	h.SetXMPPConnected(true)
	// languages ready nunca setado

	rec := httptest.NewRecorder()
	h.ServeReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHealth_ServeReadyz_CanFlipBackToNotReady(t *testing.T) {
	t.Parallel()

	h := observability.NewHealth()
	h.SetXMPPConnected(true)
	h.SetLanguagesReady(true)
	h.SetXMPPConnected(false) // ex: reconexão em andamento

	rec := httptest.NewRecorder()
	h.ServeReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
