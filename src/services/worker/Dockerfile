# syntax=docker/dockerfile:1

# --- Builder: Debian (glibc) ---
FROM golang:1.26-bookworm AS builder

ARG TARGETARCH

WORKDIR /workspace
COPY src/services/worker/go.mod src/services/worker/go.sum src/services/worker/
COPY src/services/shared/go.mod src/services/shared/go.sum src/services/shared/
COPY src/services/api/go.mod src/services/api/go.sum src/services/api/
RUN printf 'go 1.26.0\nuse (\n\t./src/services/api\n\t./src/services/worker\n\t./src/services/shared\n)\n' > go.work
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY src/services/worker src/services/worker
COPY src/services/shared src/services/shared
COPY src/services/api src/services/api
COPY src/personas src/personas

ENV CGO_ENABLED=1
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /worker ./src/services/worker/cmd/worker

# --- Runtime: Debian slim (glibc) ---
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata curl && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh \
    && cp /root/.local/bin/rtk /usr/local/bin/rtk \
    && chmod 755 /usr/local/bin/rtk \
    && rm -rf /root/.local/bin/rtk

COPY --from=builder /worker /usr/local/bin/worker
COPY --from=builder /workspace/src/personas /usr/local/share/arkloop/personas
ENV ARKLOOP_PERSONAS_ROOT=/usr/local/share/arkloop/personas
ENTRYPOINT ["worker"]
