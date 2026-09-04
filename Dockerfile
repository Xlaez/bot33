FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi
COPY web/ ./
ARG GIT_SHA=unknown
RUN echo "build ${GIT_SHA}" > /web/public-build-id.txt && npm run build && cp /web/public-build-id.txt /web/dist/build-id.txt

FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG CMD_PATH=./cmd/watcher
ARG GIT_SHA=unknown
RUN CGO_ENABLED=0 go build -o /out/app ${CMD_PATH}

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/app /app/bot33
COPY configs /app/configs
COPY --from=web /web/dist /app/web/dist
ENV WALLETS_SEED_PATH=/app/configs/wallets.seed.yaml
ENV COLLECTIONS_PATH=/app/configs/collections.yaml
EXPOSE 8080
ENTRYPOINT ["/app/bot33"]
