# syntax=docker/dockerfile:1

ARG GO_IMAGE=golang:1.27-alpine

## Run the compiler on the builder's native architecture.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/xkcd-api \
      ./cmd/api

FROM alpine:3.23 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app

WORKDIR /app

COPY --from=builder /out/xkcd-api /app/xkcd-api

USER app

ENTRYPOINT ["/app/xkcd-api"]
