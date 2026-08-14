// Package xmpp conecta a um servidor XMPP, ingressa nas salas MUC
// configuradas e expõe as mensagens recebidas já normalizadas para o
// pipeline de tradução.
package xmpp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	fluux "gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"

	"github.com/Riverfount/xmpp-translate-bot/internal/observability"
)

// Client é o contrato de conexão com o servidor XMPP.
type Client interface {
	// Start conecta, autentica, ingressa nas salas configuradas e bloqueia
	// até que ctx seja cancelado ou ocorra um erro fatal de conexão.
	Start(ctx context.Context) error
	// SendGroup envia uma mensagem de groupchat para a sala informada (JID bare).
	SendGroup(room, body string) error
	// Incoming expõe as mensagens recebidas nas salas, já normalizadas.
	Incoming() <-chan IncomingMessage
}

// IncomingMessage é uma mensagem de MUC já normalizada.
type IncomingMessage struct {
	Room      string
	FromNick  string
	Body      string
	Timestamp time.Time
	IsSelf    bool
}

// Config reúne os parâmetros necessários para conectar e ingressar em salas MUC.
type Config struct {
	JID      string
	Password string
	Server   string
	TLS      bool
	Rooms    []string
	Nickname string
}

const incomingBuffer = 32

type client struct {
	cfg      Config
	logger   *slog.Logger
	metrics  *observability.Metrics
	health   *observability.Health
	incoming chan IncomingMessage
	xc       *fluux.Client

	// connectCount conta quantas vezes joinRooms rodou: a primeira é a
	// conexão inicial, as seguintes são reconexões (StreamManager.resume).
	// Só é tocado a partir de joinRooms, que a lib nunca chama concorrentemente.
	connectCount int
}

// New cria um Client XMPP. A conexão real só ocorre em Start.
func New(cfg Config, logger *slog.Logger, metrics *observability.Metrics, health *observability.Health) Client {
	return &client{
		cfg:      cfg,
		logger:   logger,
		metrics:  metrics,
		health:   health,
		incoming: make(chan IncomingMessage, incomingBuffer),
	}
}

func (c *client) Incoming() <-chan IncomingMessage {
	return c.incoming
}

func (c *client) SendGroup(room, body string) error {
	if c.xc == nil {
		return errors.New("xmpp: client não conectado")
	}
	msg := stanza.Message{
		Attrs: stanza.Attrs{To: room, Type: stanza.MessageTypeGroupchat},
		Body:  body,
	}
	return c.xc.Send(msg)
}

func (c *client) Start(ctx context.Context) error {
	router := fluux.NewRouter()
	router.HandleFunc("message", c.handleMessage)

	xmppCfg := &fluux.Config{
		TransportConfiguration: fluux.TransportConfiguration{
			Address: c.cfg.Server,
		},
		Jid:        c.cfg.JID,
		Credential: fluux.Password(c.cfg.Password),
		Insecure:   !c.cfg.TLS,
	}

	xc, err := fluux.NewClient(xmppCfg, router, c.handleError)
	if err != nil {
		return fmt.Errorf("xmpp: criando client: %w", err)
	}
	c.xc = xc

	// O StreamManager não reingressa em salas MUC automaticamente na
	// reconexão — XEP-0045 é responsabilidade do cliente, não da stream
	// management (XEP-0198) da lib. joinRooms roda como PostConnect tanto na
	// conexão inicial quanto em cada resume() após queda, cobrindo os dois
	// casos sem lógica extra de rejoin.
	sm := fluux.NewStreamManager(xc, c.joinRooms)

	runErr := make(chan error, 1)
	go func() { runErr <- sm.Run() }()

	select {
	case <-ctx.Done():
		// sm.Stop() -> Disconnect() -> Close() só fecha o socket depois de
		// esperar até ConnectTimeout (default 15s) pela tag de stream-close
		// do peer. Se ctx for cancelado com um handshake ainda em andamento
		// (peer aceitou o TCP mas nunca respondeu), o Close() concorrente
		// interrompe a leitura bloqueada em StartStream/NewSession, que por
		// sua vez chama Close() de novo — dobrando a espera para ~2x
		// ConnectTimeout antes do processo conseguir sair.
		sm.Stop()
		return <-runErr
	case err := <-runErr:
		return err
	}
}

func (c *client) joinRooms(s fluux.Sender) {
	c.connectCount++
	if c.connectCount == 1 {
		c.logger.Info("xmpp_connected")
	} else {
		c.logger.Info("xmpp_reconnected", "attempt", c.connectCount-1)
		c.metrics.XMPPReconnectsTotal.Inc()
	}
	c.health.SetXMPPConnected(true)

	for _, room := range c.cfg.Rooms {
		presence := stanza.Presence{
			Attrs: stanza.Attrs{To: room + "/" + c.cfg.Nickname},
			Extensions: []stanza.PresExtension{
				stanza.MucPresence{
					History: stanza.History{MaxStanzas: stanza.NewNullableInt(0)},
				},
			},
		}
		if err := s.Send(presence); err != nil {
			c.logger.Warn("xmpp_join_failed", "room", room, "error", err.Error())
			continue
		}
		c.logger.Info("xmpp_room_joined", "room", room)
	}
}

func (c *client) handleMessage(_ fluux.Sender, p stanza.Packet) {
	msg, ok := p.(stanza.Message)
	if !ok {
		return
	}
	im, ok := normalizeIncoming(msg, c.cfg.Nickname)
	if !ok {
		return
	}
	c.incoming <- im
}

func (c *client) handleError(err error) {
	c.logger.Warn("xmpp_error", "error", err.Error())
	c.health.SetXMPPConnected(false)
}
