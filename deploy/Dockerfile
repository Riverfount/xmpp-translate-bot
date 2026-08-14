# Compatível com `docker build` e `podman build` — buildar sempre com o
# contexto na raiz do repo: `-f deploy/Dockerfile .`
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/bot ./cmd/bot

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/bot /bot
USER nonroot:nonroot
EXPOSE 9090
ENTRYPOINT ["/bot"]
