FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates postgresql-client \
  && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY go.mod ./
COPY supabase supabase
# dummy.ReadRepo resolves fixtures from the compile-time source path /src.
COPY agents/examples /src/agents/examples
COPY agents/fixtures /src/agents/fixtures
COPY --from=build /out/api /usr/local/bin/api
RUN mkdir -p /app/var/storage
EXPOSE 8000
CMD ["api"]
