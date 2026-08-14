package pipeline_test

// Este arquivo cobre, um a um, os cenários de aceitação de tradução:
// sucesso, timeout, 401, 429, idioma não suportado, e origem == destino —
// sempre com Detector/Translator mockados, nunca o LibreTranslate real. Cada
// teste corresponde a exatamente um cenário, pra rastreabilidade direta
// entre comportamento esperado e teste.

import (
	"context"
	"errors"
	"testing"

	"github.com/Riverfount/xmpp-translate-bot/internal/pipeline"
	"github.com/Riverfount/xmpp-translate-bot/internal/translate"
)

func TestScenario_Sucesso(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.95},
		fakeTranslator{translated: "Olá, como você está?"},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello, how are you?"})

	got := responder.awaitOne(t)
	want := "Tradução (en → pt): Olá, como você está?"
	if got.body != want {
		t.Errorf("body = %q, want %q", got.body, want)
	}
}

func TestScenario_OrigemIgualDestino(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "pt", confidence: 0.9},
		fakeTranslator{err: errors.New("Translate não deveria ser chamado quando origem == destino")},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Já em português"})

	got := responder.awaitOne(t)
	want := "A mensagem já está em pt."
	if got.body != want {
		t.Errorf("body = %q, want %q", got.body, want)
	}
}

func TestScenario_IdiomaNaoSuportado(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{err: &translate.HTTPError{StatusCode: 400}},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	if got.body != pipeline.MsgUnsupportedLanguage {
		t.Errorf("body = %q, want %q", got.body, pipeline.MsgUnsupportedLanguage)
	}
}

func TestScenario_ErroDeAutenticacao(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{err: &translate.HTTPError{StatusCode: 401}},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	if got.body != pipeline.MsgAuthError {
		t.Errorf("body = %q, want %q", got.body, pipeline.MsgAuthError)
	}
}

func TestScenario_RateLimitEsgotado(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{err: &translate.HTTPError{StatusCode: 429}},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	if got.body != pipeline.MsgServiceUnavailable {
		t.Errorf("body = %q, want %q", got.body, pipeline.MsgServiceUnavailable)
	}
}

func TestScenario_Timeout(t *testing.T) {
	t.Parallel()

	responder := newSyncResponder()
	d, _ := newTestDispatcher(t,
		fakeDetector{lang: "en", confidence: 0.9},
		fakeTranslator{err: context.DeadlineExceeded},
		responder,
	)

	d.Submit(pipeline.TranslationJob{Room: "sala@conf", Text: "Hello"})

	got := responder.awaitOne(t)
	if got.body != pipeline.MsgTimeout {
		t.Errorf("body = %q, want %q", got.body, pipeline.MsgTimeout)
	}
}
