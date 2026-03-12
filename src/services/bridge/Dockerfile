FROM golang:1.26-alpine AS builder
WORKDIR /build
# Copy workspace files
COPY go.work go.work.sum ./
COPY src/services/bridge/ src/services/bridge/
COPY src/services/shared/ src/services/shared/
COPY install/ install/
RUN cd src/services/bridge && go build -o /bridge ./cmd/bridge/

FROM alpine:3.21
RUN apk add --no-cache docker-cli docker-cli-compose
COPY --from=builder /bridge /usr/local/bin/bridge
COPY install/modules.yaml /etc/arkloop/modules.yaml
ENTRYPOINT ["bridge"]
