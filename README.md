# xmpp-translate-bot

🌐 [Português](#português) | [English](#english)

## Português

Bot de tradução para salas XMPP (MUC), usando LibreTranslate self-hosted como
backend de detecção de idioma e tradução.

O projeto foi construído em fases incrementais: fundação, XMPP mínimo,
mention parser, client LibreTranslate, pipeline, resiliência, e
empacotamento/deploy.

### Configuração

Todas as chaves são configuráveis por variável de ambiente (vencem sobre
YAML quando ambos presentes) — ver `internal/config/config.go` e
`configs/config.example.yaml` pra lista completa. `XMPP_PASSWORD`,
`LT_API_KEY` e `INFLUX_TOKEN` são só-env, nunca em YAML.

### Build e testes

```sh
make build   # bin/bot
make test
make vet
make run     # go run ./cmd/bot
```

### Container

`deploy/Dockerfile` é multi-stage (build → distroless, binário estático,
`USER nonroot`) e funciona com Docker ou Podman:

```sh
make docker-build                  # docker, por padrão
make docker-build ENGINE=podman    # ou podman
```

Expõe `/metrics` (Prometheus), `/healthz` (liveness) e `/readyz` (readiness:
conectado ao XMPP + idiomas do LibreTranslate carregados) em `METRICS_ADDR`
(`:9090` por padrão).

#### Subindo com compose (bot + InfluxDB2 local)

`compose.yaml` sobe o bot e uma instância local do InfluxDB2 — só pra testar
o writer assíncrono de eventos de tradução sem depender do isaCloud de
produção. Compatível com `docker compose` e `podman compose` /
`podman-compose`.

```sh
cp .env.example .env   # preencha os valores
docker compose up --build
```

#### systemd (alternativa ao container)

`deploy/bot.service` espera o binário em `/usr/local/bin/bot` e as variáveis
de ambiente em `/etc/xmpp-translate-bot/env` (fora do repositório, `600`).

---

## English

Translation bot for XMPP chat rooms (MUC), using self-hosted LibreTranslate
as the language detection and translation backend.

The project was built in incremental phases: foundation, minimal XMPP,
mention parser, LibreTranslate client, pipeline, resilience, and
packaging/deploy.

### Configuration

All keys are configurable via environment variable (which take precedence
over YAML when both are present) — see `internal/config/config.go` and
`configs/config.example.yaml` for the complete list. `XMPP_PASSWORD`,
`LT_API_KEY` and `INFLUX_TOKEN` are env-only, never in YAML.

### Build and tests

```sh
make build   # bin/bot
make test
make vet
make run     # go run ./cmd/bot
```

### Container

`deploy/Dockerfile` is multi-stage (build → distroless, static binary,
`USER nonroot`) and works with Docker or Podman:

```sh
make docker-build                  # docker, by default
make docker-build ENGINE=podman    # or podman
```

Exposes `/metrics` (Prometheus), `/healthz` (liveness) and `/readyz`
(readiness: connected to XMPP + LibreTranslate languages loaded) on
`METRICS_ADDR` (`:9090` by default).

#### Running with compose (bot + local InfluxDB2)

`compose.yaml` brings up the bot and a local InfluxDB2 instance — just to
test the async translation-events writer without depending on the
production isaCloud. Compatible with `docker compose` and
`podman compose` / `podman-compose`.

```sh
cp .env.example .env   # fill in the values
docker compose up --build
```

#### systemd (alternative to container)

`deploy/bot.service` expects the binary at `/usr/local/bin/bot` and
environment variables at `/etc/xmpp-translate-bot/env` (outside the
repository, `600`).
