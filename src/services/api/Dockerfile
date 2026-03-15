# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY src/services/api/go.mod src/services/api/go.sum src/services/api/
COPY src/services/shared/go.mod src/services/shared/go.sum src/services/shared/
RUN printf 'go 1.26.0\nuse (\n\t./src/services/api\n\t./src/services/shared\n)\n' > go.work
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY src/services/api src/services/api
COPY src/services/shared src/services/shared
COPY src/personas src/personas
COPY src/skills src/skills
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /api ./src/services/api/cmd/api && \
    go build -ldflags="-s -w" -o /migrate ./src/services/api/cmd/migrate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=builder /api     /usr/local/bin/api
COPY --from=builder /migrate /usr/local/bin/migrate
COPY --from=builder /workspace/src/personas /usr/local/share/arkloop/personas
COPY --from=builder /workspace/src/skills /usr/local/share/arkloop/skills
ENV ARKLOOP_PERSONAS_ROOT=/usr/local/share/arkloop/personas
ENV ARKLOOP_SKILLS_ROOT=/usr/local/share/arkloop/skills
ENTRYPOINT ["api"]
