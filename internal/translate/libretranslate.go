package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const backoffBase = 200 * time.Millisecond

var (
	_ Translator = (*Client)(nil)
	_ Detector   = (*Client)(nil)
)

// HTTPError representa uma resposta HTTP não-200 do LibreTranslate.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("translate: resposta %d do LibreTranslate: %s", e.StatusCode, e.Body)
}

// ErrNoDetectionResult é retornado quando /detect responde sem nenhum
// resultado.
var ErrNoDetectionResult = errors.New("translate: nenhum resultado de detecção retornado")

// Client é o client HTTP para um LibreTranslate self-hosted.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	maxRetries int
	logger     *slog.Logger
	sleep      func(ctx context.Context, d time.Duration) error
}

// Option customiza um Client criado por NewClient.
type Option func(*Client)

// WithSleepFunc substitui a espera de backoff entre tentativas — usado em
// teste pra não depender de tempo real.
func WithSleepFunc(f func(ctx context.Context, d time.Duration) error) Option {
	return func(c *Client) { c.sleep = f }
}

// NewClient cria um client para o LibreTranslate em baseURL, autenticando
// com apiKey em cada requisição. maxRetries é o número de tentativas extras
// além da primeira, para erros transitórios (timeout, conexão recusada,
// 429, 5xx).
func NewClient(baseURL, apiKey string, timeout time.Duration, maxRetries int, logger *slog.Logger, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		maxRetries: maxRetries,
		logger:     logger,
		sleep:      defaultSleep,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type detectResult struct {
	Confidence float64 `json:"confidence"`
	Language   string  `json:"language"`
}

// Detect implementa Detector via POST /detect.
func (c *Client) Detect(ctx context.Context, text string) (string, float64, error) {
	reqBody := map[string]string{"q": text, "api_key": c.apiKey}

	var results []detectResult
	if err := c.doJSON(ctx, http.MethodPost, "/detect", reqBody, &results); err != nil {
		return "", 0, err
	}
	if len(results) == 0 {
		return "", 0, ErrNoDetectionResult
	}

	best := results[0]
	for _, r := range results[1:] {
		if r.Confidence > best.Confidence {
			best = r
		}
	}
	return best.Language, best.Confidence / 100, nil
}

type translateRequest struct {
	Q      string `json:"q"`
	Source string `json:"source"`
	Target string `json:"target"`
	Format string `json:"format"`
	APIKey string `json:"api_key"`
}

type translateResponse struct {
	TranslatedText string `json:"translatedText"`
}

// Translate implementa Translator via POST /translate.
func (c *Client) Translate(ctx context.Context, text, source, target string) (string, error) {
	if source == "" {
		source = "auto"
	}
	reqBody := translateRequest{Q: text, Source: source, Target: target, Format: "text", APIKey: c.apiKey}

	var resp translateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/translate", reqBody, &resp); err != nil {
		return "", err
	}
	return resp.TranslatedText, nil
}

type languageEntry struct {
	Code string `json:"code"`
}

// SupportedLanguages implementa Translator via GET /languages.
func (c *Client) SupportedLanguages(ctx context.Context) ([]string, error) {
	var entries []languageEntry
	if err := c.doJSON(ctx, http.MethodGet, "/languages", nil, &entries); err != nil {
		return nil, err
	}

	codes := make([]string, len(entries))
	for i, e := range entries {
		codes[i] = e.Code
	}
	return codes, nil
}

// doJSON executa uma requisição JSON contra o LibreTranslate, com retry para
// erros transitórios (timeout, conexão recusada, 429, 5xx) e backoff
// exponencial com jitter entre tentativas. Nunca tenta de novo em 400/401/403.
func (c *Client) doJSON(ctx context.Context, method, path string, reqBody, respBody any) error {
	var bodyBytes []byte
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("translate: codificando request: %w", err)
		}
		bodyBytes = b
	}

	attempts := 1 + c.maxRetries
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := c.sleep(ctx, backoffDelay(attempt-2)); err != nil {
				return lastErr
			}
		}

		retryable, err := c.doOnce(ctx, method, path, bodyBytes, respBody)
		if err == nil {
			return nil
		}
		lastErr = err

		c.logger.Warn("translate_request_failed",
			"method", method,
			"path", path,
			"attempt", attempt,
			"retryable", retryable,
			"error", err.Error(),
		)

		if !retryable {
			return err
		}
	}
	return lastErr
}

func (c *Client) doOnce(ctx context.Context, method, path string, bodyBytes []byte, respBody any) (retryable bool, err error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return false, fmt.Errorf("translate: montando request: %w", err)
	}
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return true, fmt.Errorf("translate: requisição falhou: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return isRetryableStatus(resp.StatusCode), &HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return false, fmt.Errorf("translate: decodificando resposta: %w", err)
		}
	}
	return false, nil
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// backoffDelay calcula o atraso antes da tentativa de índice n+2 (n=0 para a
// primeira retentativa): base*2^n, com jitter aleatório de ±20%.
func backoffDelay(n int) time.Duration {
	d := backoffBase << n
	jitter := time.Duration((rand.Float64()*0.4 - 0.2) * float64(d))
	return d + jitter
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
