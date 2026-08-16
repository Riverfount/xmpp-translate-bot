# Contribuindo / Contributing

🌐 [Português](#português) | [English](#english)

## Português

Obrigado pelo interesse em contribuir com o xmpp-translate-bot! Este documento
resume como reportar problemas, propor mudanças e submeter pull requests.

### Reportando bugs e propondo melhorias

Use as [Issues do GitHub](https://github.com/Riverfount/xmpp-translate-bot/issues).
Para bugs, inclua passos para reproduzir, comportamento esperado vs. observado
e versão do Go/SO quando relevante. Para melhorias, descreva o problema que a
mudança resolve antes de propor a solução.

### Pré-requisitos

- Go na versão declarada em `go.mod`
- Docker ou Podman, se for testar a imagem/`compose.yaml`

### Fluxo de desenvolvimento

1. Atualize a `main` local: `git switch main && git pull`.
2. Crie uma branch a partir da issue correspondente: `git switch -c issue-<numero>`.
3. Desenvolva em TDD: escreva um teste que falhe antes do código que o faz
   passar, depois refatore.
4. Abra um pull request contra `main` referenciando a issue (`Closes #<numero>`).

Cada pull request é revisado e mergeado via GitHub; não dá squash/rebase por
fora desse fluxo.

### Padrões de código

- Formatação via `gofmt`/`goimports` (cobertos pelo `golangci-lint`, ver
  `.golangci.yml`) — rode `make vet` antes de abrir o PR.
- Nomes idiomáticos e proporcionais ao escopo: curtos em variáveis de loop e
  receivers, descritivos em identificadores exportados ou de escopo amplo.
- Comentários só para decisões não óbvias (motivo de um workaround, invariante
  escondida) — não para narrar o que o código já deixa claro.
- Segredos (senhas, tokens) nunca em código, YAML ou log — só variável de
  ambiente.

### Testes

```sh
make test          # go test ./...
go test ./... -race -cover   # o que o CI roda
```

Use `t.Parallel()` em testes independentes/seguros; evite combiná-lo com
`t.Setenv`, que exige execução serial.

### Commits

Mensagens seguem o padrão `tipo: descrição` (`feat`, `fix`, `docs`, `test`,
`chore`, `ci`), no imperativo, descrevendo o *porquê* quando não for óbvio —
veja o histórico (`git log`) para exemplos.

### CI

Todo PR roda build, `go vet`, `go test -race -cover` e `golangci-lint`. Os
quatro precisam passar antes do merge.

---

## English

Thanks for your interest in contributing to xmpp-translate-bot! This document
summarizes how to report issues, propose changes and submit pull requests.

### Reporting bugs and suggesting enhancements

Use [GitHub Issues](https://github.com/Riverfount/xmpp-translate-bot/issues).
For bugs, include steps to reproduce, expected vs. observed behavior, and
Go/OS version when relevant. For enhancements, describe the problem the
change solves before proposing the solution.

### Prerequisites

- Go at the version declared in `go.mod`
- Docker or Podman, if you're testing the image/`compose.yaml`

### Development workflow

1. Update local `main`: `git switch main && git pull`.
2. Create a branch from the corresponding issue: `git switch -c issue-<number>`.
3. Develop using TDD: write a failing test before the code that makes it
   pass, then refactor.
4. Open a pull request against `main` referencing the issue (`Closes #<number>`).

Every pull request is reviewed and merged through GitHub; no squash/rebase
outside that flow.

### Code style

- Formatting via `gofmt`/`goimports` (enforced by `golangci-lint`, see
  `.golangci.yml`) — run `make vet` before opening the PR.
- Idiomatic names proportional to scope: short for loop variables and
  receivers, descriptive for exported or broad-scope identifiers.
- Comments only for non-obvious decisions (the reason for a workaround, a
  hidden invariant) — not to narrate what the code already makes clear.
- Secrets (passwords, tokens) never in code, YAML, or logs — env variables
  only.

### Tests

```sh
make test          # go test ./...
go test ./... -race -cover   # what CI runs
```

Use `t.Parallel()` for independent/safe tests; avoid combining it with
`t.Setenv`, which requires serial execution.

### Commits

Messages follow the `type: description` pattern (`feat`, `fix`, `docs`,
`test`, `chore`, `ci`), imperative mood, describing *why* when it isn't
obvious — see the history (`git log`) for examples.

### CI

Every PR runs build, `go vet`, `go test -race -cover`, and `golangci-lint`.
All four must pass before merging.
