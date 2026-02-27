# Sandbox 服务需要宿主机具备 KVM 支持和 Firecracker 二进制。
# 运行容器时必须挂载 /dev/kvm 设备并以 privileged 模式运行：
#   docker run --device /dev/kvm -v /usr/bin/firecracker:/usr/bin/firecracker ...
#
# 内核镜像和 rootfs 需单独构建并挂载进容器：
#   -v /path/to/vmlinux:/opt/sandbox/vmlinux
#   -v /path/to/rootfs.ext4:/opt/sandbox/rootfs.ext4
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY src/services/sandbox/go.mod src/services/sandbox/go.sum src/services/sandbox/
RUN printf 'go 1.26.0\nuse (\n\t./src/services/sandbox\n)\n' > go.work
RUN go mod download
COPY src/services/sandbox src/services/sandbox
RUN go build -ldflags="-s -w" -o /sandbox     ./src/services/sandbox/cmd/sandbox
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o /sandbox-agent ./src/services/sandbox/agent

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=builder /sandbox       /usr/local/bin/sandbox
COPY --from=builder /sandbox-agent /usr/local/share/arkloop/sandbox/sandbox-agent
ENV ARKLOOP_SANDBOX_ADDR=0.0.0.0:8002
ENV ARKLOOP_SANDBOX_SOCKET_DIR=/run/sandbox
ENTRYPOINT ["sandbox"]
