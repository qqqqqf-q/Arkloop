FROM node:22-alpine AS builder
WORKDIR /workspace
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY src/apps/shared/package.json src/apps/shared/package.json
COPY src/apps/console/package.json src/apps/console/package.json
RUN pnpm install --frozen-lockfile
COPY src/apps/shared src/apps/shared
COPY src/apps/console src/apps/console
RUN pnpm --filter console build

FROM nginx:1.27-alpine
COPY src/apps/shared/nginx-spa.conf.template /etc/nginx/templates/default.conf.template
COPY --from=builder /workspace/src/apps/console/dist /usr/share/nginx/html
ENV API_UPSTREAM=http://api:19001
HEALTHCHECK CMD wget -qO- http://127.0.0.1/healthz >/dev/null || exit 1
