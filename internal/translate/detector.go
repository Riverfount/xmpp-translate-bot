package translate

import (
	"errors"
	"fmt"
)

// NewDetector retorna a implementação de Detector configurada por kind:
// "libretranslate" (default) reusa client, batendo em POST /detect no mesmo
// servidor usado para tradução. "local" ainda não está implementada.
func NewDetector(kind string, client *Client) (Detector, error) {
	switch kind {
	case "", "libretranslate":
		return client, nil
	case "local":
		return nil, errors.New("translate: detector \"local\" ainda não implementado")
	default:
		return nil, fmt.Errorf("translate: detector desconhecido: %q", kind)
	}
}
