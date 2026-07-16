# PitchMarket E2 backend — long-running process (REST/WS, SSE feeds, crank).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/server /app/server
COPY fixtures /app/fixtures
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
# /data persists runtime state: TxLINE credential cache, demo USDC mint address
VOLUME /data
ENV TXODDS_CACHE=/data/txline-credentials.json
EXPOSE 8080
ENTRYPOINT ["/app/entrypoint.sh"]
