# Customer-app production image — Next.js 16 with shadcn UI.
# Multi-stage so the runtime image stays small and unprivileged.
#
# This is the CUSTOMER-FACING app. Sister image is the operator dashboard at
# apps/dashboard/Dockerfile. Both share the same Postgres + better-auth instance.

FROM node:20-alpine AS deps
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate

# Copy workspace manifests first so the install layer caches well.
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/customer-app/package.json apps/customer-app/
COPY apps/dashboard/package.json apps/dashboard/
COPY packages/sdk-py/pyproject.toml packages/sdk-py/
COPY packages/sdk-ts/package.json packages/sdk-ts/
COPY apps/backend/pyproject.toml apps/backend/

RUN pnpm install --frozen-lockfile --filter=customer-app...

FROM node:20-alpine AS build
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate

COPY --from=deps /app /app
COPY apps/customer-app apps/customer-app
COPY brand.yaml brand.yaml
COPY scripts/generate-brand.mjs scripts/generate-brand.mjs
COPY tsconfig.base.json ./

# Recreate workspace package links after copying the app source. Docker's
# directory overlay can mask pnpm's app-local symlinks from the deps stage.
RUN pnpm install --frozen-lockfile --filter=customer-app...

WORKDIR /app/apps/customer-app
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

FROM node:20-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

RUN addgroup -g 1001 -S app && adduser -S -u 1001 -G app app

COPY --from=build --chown=app:app /app /app

USER app
WORKDIR /app/apps/customer-app
EXPOSE 3000

CMD ["node_modules/.bin/next", "start", "-p", "3000"]
