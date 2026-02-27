FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY src/services/api/go.mod src/services/api/go.sum src/services/api/
COPY src/services/shared/go.mod src/services/shared/go.sum src/services/shared/
RUN printf 'go 1.26.0\nuse (\n\t./src/services/api\n\t./src/services/shared\n)\n' > go.work
RUN go mod download
COPY src/services/api src/services/api
COPY src/services/shared src/services/shared
RUN go build -ldflags="-s -w" -o /api     ./src/services/api/cmd/api
RUN go build -ldflags="-s -w" -o /migrate ./src/services/api/cmd/migrate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=builder /api     /usr/local/bin/api
COPY --from=builder /migrate /usr/local/bin/migrate
ENTRYPOINT ["api"]
