// Package translate isola a integração HTTP com o LibreTranslate atrás de
// interfaces, para permitir mock em teste e trocar a implementação de
// detecção sem tocar no resto do pipeline.
package translate

import "context"

// Detector identifica o idioma de origem de um texto.
type Detector interface {
	// Detect retorna o código ISO do idioma e a confiança normalizada [0..1].
	Detect(ctx context.Context, text string) (lang string, confidence float64, err error)
}

// Translator traduz texto e expõe os idiomas suportados pelo serviço.
type Translator interface {
	// Translate traduz text de source para target. source pode ser "" (auto).
	Translate(ctx context.Context, text, source, target string) (string, error)
	// SupportedLanguages lista os códigos de idioma suportados, para validação.
	SupportedLanguages(ctx context.Context) ([]string, error)
}
