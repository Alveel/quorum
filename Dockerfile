# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Install templ for code generation.
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY . .
RUN templ generate
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/server ./cmd/server

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM registry.access.redhat.com/ubi9-micro:latest

COPY --from=builder /bin/server /bin/server

EXPOSE 8080
USER 1001

ENTRYPOINT ["/bin/server"]
