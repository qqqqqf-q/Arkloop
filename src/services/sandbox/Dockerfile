# syntax=docker/dockerfile:1
# Sandbox 服务需要宿主机具备 KVM 支持和 Firecracker 二进制。
# 运行容器时必须挂载 /dev/kvm 设备和 Firecracker 二进制：
#   docker run --device /dev/kvm -v /usr/bin/firecracker:/usr/bin/firecracker ...
#
# 内核镜像和 rootfs 需单独构建并挂载进容器：
#   -v /path/to/vmlinux:/opt/sandbox/vmlinux
#   -v /path/to/rootfs.ext4:/opt/sandbox/rootfs.ext4
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
RUN apk add --no-cache ca-certificates tzdata wget iproute2 iptables
COPY --from=builder /sandbox       /usr/local/bin/sandbox
COPY --from=builder /sandbox-agent /usr/local/share/arkloop/sandbox/sandbox-agent
ENV ARKLOOP_SANDBOX_ADDR=0.0.0.0:8002
ENV ARKLOOP_SANDBOX_SOCKET_DIR=/run/sandbox
ENTRYPOINT ["sandbox"]
