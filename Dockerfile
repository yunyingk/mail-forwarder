FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o mail-forwarder .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /build/mail-forwarder /usr/local/bin/mail-forwarder

USER nobody

ENTRYPOINT ["mail-forwarder"]
CMD ["-config", "/etc/mail-forwarder/config.yaml"]
