FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
    -o grc ./cmd/grc

FROM alpine:3.21

RUN apk add --no-cache ca-certificates git && \
    adduser -D -u 1000 grc

WORKDIR /data
COPY --from=builder /app/grc /usr/local/bin/grc

USER grc

ENTRYPOINT ["grc"]
