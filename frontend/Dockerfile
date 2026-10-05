# Frontend image: the built site served by nginx, which also proxies /api to
# the backend (API_UPSTREAM, default http://api:8080).
#
#   docker build -t tft-platform-frontend frontend
#
# Game icons (~100 MB, from Riot's Data Dragon) are downloaded at build time
# so the image is self-contained; pass --build-arg FETCH_ASSETS=0 to skip
# them (the UI then falls back to CommunityDragon icons and text).

FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG FETCH_ASSETS=1
RUN if [ "$FETCH_ASSETS" = "1" ]; then npm run assets; fi
RUN npm run build

FROM nginx:1.27-alpine
# The official image renders /etc/nginx/templates/*.template with envsubst
# at startup, filling in API_UPSTREAM.
ENV API_UPSTREAM=http://api:8080
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
