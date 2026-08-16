# syntax=docker/dockerfile:1
# Build stage — cross-compiles a static binary for the target platform.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/ejina-microgrid .

# Runtime stage — minimal image with only the binary and config.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=builder /out/ejina-microgrid /app/ejina-microgrid
COPY config.json /app/config.json
RUN mkdir -p /app/data && chown -R app:app /app
USER app
EXPOSE 49495
ENTRYPOINT ["/app/ejina-microgrid"]
