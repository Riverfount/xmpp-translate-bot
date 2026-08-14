# xmpp-translate-bot

Bot de tradução para salas XMPP (MUC), usando LibreTranslate self-hosted como
backend de detecção de idioma e tradução.

O projeto foi construído seguindo um plano de implementação faseado derivado
da especificação técnica do produto (§16): fundação, XMPP mínimo, mention
parser, client LibreTranslate, pipeline, resiliência, e empacotamento/deploy.

## Configuração

Todas as chaves são configuráveis por variável de ambiente (vencem sobre
YAML quando ambos presentes) — ver `internal/config/config.go` e
`configs/config.example.yaml` pra lista completa. `XMPP_PASSWORD`,
`LT_API_KEY` e `INFLUX_TOKEN` são só-env, nunca em YAML.

## Build e testes

```
make build   # bin/bot
make test
make vet
make run     # go run ./cmd/bot
```

## Container

`deploy/Dockerfile` é multi-stage (build → distroless, binário estático,
`USER nonroot`) e funciona com Docker ou Podman:

```
make docker-build                  # docker, por padrão
make docker-build ENGINE=podman    # ou podman
```

Expõe `/metrics` (Prometheus), `/healthz` (liveness) e `/readyz` (readiness:
conectado ao XMPP + idiomas do LibreTranslate carregados) em `METRICS_ADDR`
(`:9090` por padrão).

### Subindo com compose (bot + InfluxDB2 local)

`compose.yaml` sobe o bot e uma instância local do InfluxDB2 — só pra testar
a extensão pós-spec (writer assíncrono de eventos de tradução) sem depender
do isaCloud de produção. Compatível com `docker compose` e `podman compose`
/ `podman-compose`.

```
cp .env.example .env   # preencha os valores
docker compose up --build
```

### systemd (alternativa ao container)

`deploy/bot.service` espera o binário em `/usr/local/bin/bot` e as variáveis
de ambiente em `/etc/xmpp-translate-bot/env` (fora do repositório, `600`).
