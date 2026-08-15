package lookup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	backoffBase = 200 * time.Millisecond
	// maxErrorBody limita quanto do corpo de erro entra no HTTPError — o
	// suficiente pra diagnosticar, sem despejar uma página HTTP inteira no log.
	maxErrorBody = 512
)

// httpJSON é o transporte compartilhado pelas fontes remotas: GET com
// User-Agent obrigatório, retry com backoff exponencial + jitter para erros
// transitórios (rede, 429, 5xx) e nenhum retry para 4xx determinístico.
//
// A Wikimedia exige um User-Agent descritivo com forma de contato; requisição
// sem isso leva 403. Ver https://meta.wikimedia.org/wiki/User-Agent_policy.
type httpJSON struct {
	client     *http.Client
	userAgent  string
	maxRetries int
	sleep      func(ctx context.Context, d time.Duration) error
}

func newHTTPJSON(userAgent string, timeout time.Duration, maxRetries int, transport http.RoundTripper) *httpJSON {
	return &httpJSON{
		client:     &http.Client{Timeout: timeout, Transport: transport},
		userAgent:  userAgent,
		maxRetries: maxRetries,
		sleep:      defaultSleep,
	}
}

// get busca url e decodifica o JSON em out. Retorna *HTTPError para respostas
// não-2xx que sobreviveram aos retries.
func (h *httpJSON) get(ctx context.Context, url string, out any) error {
	var lastErr error

	for attempt := 0; attempt <= h.maxRetries; attempt++ {
		if attempt > 0 {
			if err := h.sleep(ctx, backoffWithJitter(attempt)); err != nil {
				return err
			}
		}

		err := h.attempt(ctx, url, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			return err
		}
	}
	return lastErr
}

func (h *httpJSON) attempt(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", h.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// isRetryable classifica o erro: 429 e 5xx são transitórios, o resto dos
// códigos HTTP não (404 não melhora com retry). Erro de transporte é
// retentável, exceto cancelamento/deadline do próprio contexto.
func isRetryable(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

func backoffWithJitter(attempt int) time.Duration {
	d := backoffBase << (attempt - 1)
	//nolint:gosec // jitter não é uso criptográfico
	return d + time.Duration(rand.Int63n(int64(d)))
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
