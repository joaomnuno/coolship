# syntax=docker/dockerfile:1
# The documentation site as a Node.js server, built by vinext (Vite) from the
# Next.js app in this directory. Deploy on Coolify as a Dockerfile application
# with docs/ as its base directory; the server listens on $PORT (3000).
#
# Dockerfile.next builds the same site with `next build` instead; see
# README.md for when and how to switch.

FROM node:24-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

FROM node:24-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# SITE_URL is baked into llms.txt and the per-page Markdown at build time
# (and read again at runtime, see site.config.mjs).
ARG SITE_URL=https://coolship.itrocas.com
ENV SITE_URL=$SITE_URL
RUN npm run build

FROM node:24-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
# The vinext server binds to HOST (Next.js standalone uses HOSTNAME).
ENV HOST=0.0.0.0
ARG SITE_URL=https://coolship.itrocas.com
ENV SITE_URL=$SITE_URL
RUN addgroup -S -g 1001 nodejs \
 && adduser -S -u 1001 -G nodejs docs
COPY --from=build --chown=docs:nodejs /app/dist/standalone ./
USER docs
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/" >/dev/null || exit 1
CMD ["node", "server.js"]
