FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY src/services/worker/go.mod src/services/worker/go.sum src/services/worker/
COPY src/services/shared/go.mod src/services/shared/go.sum src/services/shared/
RUN printf 'go 1.26.0\nuse (\n\t./src/services/worker\n\t./src/services/shared\n)\n' > go.work
RUN go mod download
COPY src/services/worker src/services/worker
COPY src/services/shared src/services/shared
COPY src/personas src/personas
RUN go build -ldflags="-s -w" -o /worker ./src/services/worker/cmd/worker

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=builder /worker /usr/local/bin/worker
COPY --from=builder /workspace/src/personas /usr/local/share/arkloop/personas
ENV ARKLOOP_PERSONAS_ROOT=/usr/local/share/arkloop/personas
ENTRYPOINT ["worker"]
