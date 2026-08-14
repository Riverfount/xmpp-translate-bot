// Package pipeline liga o parser de menção ao Detector/Translator e formata
// a resposta, com um worker pool de concorrência limitada.
package pipeline

import "time"

// TranslationJob é a unidade de trabalho de uma menção: carrega todo o
// contexto necessário pra processá-la, sem estado compartilhado com outros
// jobs — falha em um job nunca afeta outro.
type TranslationJob struct {
	Room       string
	From       string
	Text       string
	ReceivedAt time.Time
}
