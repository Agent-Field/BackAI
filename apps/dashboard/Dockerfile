# Dashboard production image — Next.js 15 with shadcn UI.
# Multi-stage to keep the final image small and unprivileged.

FROM node:20-alpine AS deps
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate

# Copy workspace manifests first so the install layer caches well.
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/dashboard/package.json apps/dashboard/
COPY packages/sdk-py/pyproject.toml packages/sdk-py/
COPY packages/sdk-ts/package.json packages/sdk-ts/
COPY apps/backend/pyproject.toml apps/backend/

RUN pnpm install --frozen-lockfile --filter=dashboard...

FROM node:20-alpine AS build
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate

COPY --from=deps /app /app
COPY apps/dashboard apps/dashboard
COPY brand.yaml brand.yaml
COPY scripts/generate-brand.mjs scripts/generate-brand.mjs
COPY tsconfig.base.json ./

WORKDIR /app/apps/dashboard
# Skip Next.js telemetry during build
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

FROM node:20-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

RUN addgroup -g 1001 -S app && adduser -S -u 1001 -G app app

# Copy the whole workspace install so all deps (better-auth, pg, etc.) are present at runtime.
COPY --from=build --chown=app:app /app /app

USER app
WORKDIR /app/apps/dashboard
EXPOSE 3000

CMD ["node_modules/.bin/next", "start", "-p", "3000"]
