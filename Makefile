.PHONY: dev test test-integration lint templ migrate build image helm-lint

DEV_DB_URL ?= postgresql://quorum:quorum@127.0.0.1:5432/quorum?sslmode=disable

# Local dev: start postgres, apply migrations, run app on :8080.
dev:
	podman-compose up -d db
	DEV_AUTH_BYPASS=true DEV_USER=devuser DEV_ADMIN=true \
	  DATABASE_URL=$(DEV_DB_URL) \
	  go run ./cmd/server

test:
	go test ./...

test-integration:
	TEST_DATABASE_URL=$(DEV_DB_URL) go test -tags=integration ./internal/store/...

lint:
	golangci-lint run

# Regenerate Go code from *.templ files. Run after editing templates.
templ:
	templ generate

# Apply migrations: start the server (which migrates then serves), then Ctrl-C.
# Or just run `make dev` — migrations run automatically on every startup.
migrate:
	DATABASE_URL=$(DEV_DB_URL) go run ./cmd/server

build:
	CGO_ENABLED=0 go build -o bin/server ./cmd/server

image:
	podman build -t quorum:dev .

helm-lint:
	helm lint deploy/helm/quorum -f deploy/helm/quorum/values.lint.yaml
	helm lint deploy/helm/quorum -f deploy/helm/quorum/values.lint.httproute-oidc.yaml
