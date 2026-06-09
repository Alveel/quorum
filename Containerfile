# ── Build stage ───────────────────────────────────────────────────────────────
ARG GO_IMAGE=registry.access.redhat.com/hi/go:1.26-builder
ARG UBI_IMAGE=registry.access.redhat.com/ubi10/ubi-micro:10.2-1779859609
FROM ${GO_IMAGE} AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/server ./cmd/server

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM ${UBI_IMAGE}

COPY --from=builder /bin/server /bin/server

EXPOSE 8080
USER 1001

ENTRYPOINT ["/bin/server"]
