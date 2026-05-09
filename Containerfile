# ── Build stage ───────────────────────────────────────────────────────────────
FROM registry.access.redhat.com/hi/go:1.26-builder AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/server ./cmd/server

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM registry.access.redhat.com/ubi10/ubi-micro:10.1-1777857595

COPY --from=builder /bin/server /bin/server

EXPOSE 8080
USER 1001

ENTRYPOINT ["/bin/server"]
