package xmpp

import (
	"encoding/xml"
	"time"

	"gosrc.io/xmpp/stanza"
)

// nsDelay é o namespace da XEP-0203 (Delayed Delivery), usado por servidores
// MUC para marcar mensagens de histórico reenviadas ao entrar na sala.
const nsDelay = "urn:xmpp:delay"

// Delay implementa XEP-0203: Delayed Delivery.
type Delay struct {
	XMLName xml.Name `xml:"urn:xmpp:delay delay"`
	From    string   `xml:"from,attr,omitempty"`
	Stamp   string   `xml:"stamp,attr,omitempty"`
}

func init() {
	stanza.TypeRegistry.MapExtension(stanza.PKTMessage, xml.Name{Space: nsDelay, Local: "delay"}, Delay{})
}

// normalizeIncoming converte uma stanza.Message em IncomingMessage. Retorna
// ok=false para mensagens que não são groupchat ou que carregam a extensão
// XEP-0203 (replay de histórico ao entrar na sala) — essas nunca chegam ao
// pipeline.
func normalizeIncoming(msg stanza.Message, ownNickname string) (im IncomingMessage, ok bool) {
	if msg.Type != stanza.MessageTypeGroupchat {
		return IncomingMessage{}, false
	}

	var delay Delay
	if msg.Get(&delay) {
		return IncomingMessage{}, false
	}

	from, err := stanza.NewJid(msg.From)
	if err != nil {
		return IncomingMessage{}, false
	}

	return IncomingMessage{
		Room:      from.Bare(),
		FromNick:  from.Resource,
		Body:      msg.Body,
		Timestamp: time.Now(),
		IsSelf:    from.Resource == ownNickname,
	}, true
}
