# CI passes the go directive from go.mod; the default only serves local builds.
ARG GO_VERSION=1.26.3
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Cross-compile on the build platform so multi-arch builds skip emulating the Go toolchain.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w -buildid=" \
    -o mcp-server-stripe-org ./cmd/mcp

FROM alpine:3.22

RUN apk --no-cache add ca-certificates && \
    addgroup -g 1001 appgroup && \
    adduser -D -u 1001 -G appgroup appuser

WORKDIR /app

COPY --from=builder /app/mcp-server-stripe-org .

USER appuser

EXPOSE 8080

ENTRYPOINT ["./mcp-server-stripe-org"]
CMD ["-http", ":8080"]
