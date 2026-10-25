FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY agents/go.mod agents/go.sum agents/
WORKDIR /src/agents
RUN go mod download
WORKDIR /src
COPY cmd cmd
COPY internal internal
COPY agents agents
WORKDIR /src/agents
RUN CGO_ENABLED=0 go build -o /out/nmc3-agent ./cmd/nmc3-agent

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates postgresql-client \
  && rm -rf /var/lib/apt/lists/*
WORKDIR /app/agents
COPY agents/opencode.jsonc ./opencode.jsonc
COPY agents/examples ./examples
COPY agents/db ./db
COPY script/docker/agent-entrypoint.sh /entrypoint.sh
COPY --from=build /out/nmc3-agent /usr/local/bin/nmc3-agent
RUN chmod +x /entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
