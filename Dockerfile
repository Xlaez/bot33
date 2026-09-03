FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG CMD_PATH=./cmd/watcher
RUN CGO_ENABLED=0 go build -o /out/app ${CMD_PATH}

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/app /app/bot33
COPY configs /app/configs
ENV WALLETS_SEED_PATH=/app/configs/wallets.seed.yaml
EXPOSE 8080
ENTRYPOINT ["/app/bot33"]
