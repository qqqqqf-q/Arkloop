# syntax=docker/dockerfile:1
# Sandbox 服务（Docker 后端）。通过挂载的用户态 Docker socket 创建 sandbox-agent 容器：
#   docker run -v /path/to/docker.sock:/run/docker-host.sock ...
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY src/services/sandbox/go.mod src/services/sandbox/go.sum src/services/sandbox/
COPY src/services/shared/go.mod src/services/shared/go.sum src/services/shared/
RUN printf 'go 1.26.0\nuse (\n\t./src/services/sandbox\n\t./src/services/shared\n)\n' > go.work
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY src/services/sandbox src/services/sandbox
COPY src/services/shared src/services/shared
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /sandbox ./src/services/sandbox/cmd/sandbox && \
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o /sandbox-agent ./src/services/sandbox/agent

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=builder /sandbox       /usr/local/bin/sandbox
COPY --from=builder /sandbox-agent /usr/local/share/arkloop/sandbox/sandbox-agent
ENV ARKLOOP_SANDBOX_ADDR=0.0.0.0:8002
ENV ARKLOOP_SANDBOX_SOCKET_DIR=/run/sandbox
ENTRYPOINT ["sandbox"]
