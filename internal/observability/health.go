package observability

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Health rastreia os sinais que compõem a readiness do bot: conexão XMPP
// ativa e o fetch inicial de /languages do LibreTranslate feito com sucesso.
// Liveness (o processo está de pé) não depende de nenhum desses sinais.
type Health struct {
	mu             sync.RWMutex
	xmppConnected  bool
	languagesReady bool
}

// NewHealth cria um Health começando como "não pronto".
func NewHealth() *Health {
	return &Health{}
}

// SetXMPPConnected atualiza se a conexão XMPP está ativa no momento.
func (h *Health) SetXMPPConnected(v bool) {
	h.mu.Lock()
	h.xmppConnected = v
	h.mu.Unlock()
}

// SetLanguagesReady atualiza se o fetch inicial de /languages já teve sucesso.
func (h *Health) SetLanguagesReady(v bool) {
	h.mu.Lock()
	h.languagesReady = v
	h.mu.Unlock()
}

// Ready reporta o estado atual dos dois sinais de readiness.
func (h *Health) Ready() (xmppConnected, languagesReady bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.xmppConnected, h.languagesReady
}

// ServeReadyz é o handler HTTP de /readyz: 200 quando os dois sinais estão
// ativos, 503 caso contrário.
func (h *Health) ServeReadyz(w http.ResponseWriter, _ *http.Request) {
	xmppConnected, languagesReady := h.Ready()

	status := http.StatusOK
	if !xmppConnected || !languagesReady {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"xmpp_connected":  xmppConnected,
		"languages_ready": languagesReady,
	})
}
