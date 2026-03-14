# syntax=docker/dockerfile:1
FROM node:22-alpine AS builder
WORKDIR /workspace
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY src/apps/shared/package.json src/apps/shared/package.json
COPY src/apps/web/package.json src/apps/web/package.json
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY src/apps/shared src/apps/shared
COPY src/apps/web src/apps/web
RUN pnpm --filter web build

FROM nginx:1.27-alpine
COPY src/apps/shared/nginx-spa.conf.template /etc/nginx/templates/default.conf.template
COPY --from=builder /workspace/src/apps/web/dist /usr/share/nginx/html
ENV API_UPSTREAM=http://gateway:19000
HEALTHCHECK CMD wget -qO- http://127.0.0.1/healthz >/dev/null || exit 1
