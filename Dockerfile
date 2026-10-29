# Husic: the web app and server in one small image.
# docker compose up -d  (see compose.yaml), or build alone with: docker build -t husic .

FROM node:22-alpine AS web
WORKDIR /src
RUN apk add --no-cache python3
COPY web/package.json web/package-lock.json web/
RUN npm --prefix web ci
COPY web web
COPY connect connect
RUN npm --prefix web run build

FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /husic ./cmd/husic

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 husic \
 && mkdir -p /data/accounts && chown -R husic /data
COPY --from=server /husic /usr/local/bin/husic
COPY --from=web /src/web/dist /app/web
USER husic
ENV HUSIC_LISTEN=0.0.0.0:18788 \
    HUSIC_WEB=/app/web \
    HUSIC_ACCOUNT_STATE=/data/accounts \
    HUSIC_DB=sqlite:/data/husic.db
VOLUME /data
EXPOSE 18788
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:18788/api/health || exit 1
ENTRYPOINT ["husic"]
