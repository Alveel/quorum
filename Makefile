.PHONY: dev test test-integration lint templ migrate build image helm-lint release release-chart

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

# Bump app version (appVersion in Chart.yaml) and create a git tag.
# TYPE=patch|minor|major (default: patch). Then: git push && git push --tags
release:
	@TYPE=$${TYPE:-patch}; \
	CURRENT=$$(grep '^appVersion:' deploy/helm/quorum/Chart.yaml | awk '{print $$2}' | tr -d '"'); \
	MAJOR=$$(echo $$CURRENT | cut -d. -f1); \
	MINOR=$$(echo $$CURRENT | cut -d. -f2); \
	PATCH=$$(echo $$CURRENT | cut -d. -f3); \
	case $$TYPE in \
	  major) NEXT="$$((MAJOR+1)).0.0" ;; \
	  minor) NEXT="$${MAJOR}.$$((MINOR+1)).0" ;; \
	  patch) NEXT="$${MAJOR}.$${MINOR}.$$((PATCH+1))" ;; \
	  *) echo "TYPE must be patch, minor, or major"; exit 1 ;; \
	esac; \
	sed -i "s/^appVersion: .*/appVersion: \"$$NEXT\"/" deploy/helm/quorum/Chart.yaml; \
	git add deploy/helm/quorum/Chart.yaml; \
	git commit -m "chore(release): v$$NEXT"; \
	git tag "v$$NEXT"; \
	echo ""; \
	echo "Tagged v$$NEXT — run: git push && git push --tags"

# Bump chart version independently (chart template/values changes, no app change).
# TYPE=patch|minor|major (default: patch). Then: git push
release-chart:
	@TYPE=$${TYPE:-patch}; \
	CURRENT=$$(grep '^version:' deploy/helm/quorum/Chart.yaml | awk '{print $$2}'); \
	MAJOR=$$(echo $$CURRENT | cut -d. -f1); \
	MINOR=$$(echo $$CURRENT | cut -d. -f2); \
	PATCH=$$(echo $$CURRENT | cut -d. -f3); \
	case $$TYPE in \
	  major) NEXT="$$((MAJOR+1)).0.0" ;; \
	  minor) NEXT="$${MAJOR}.$$((MINOR+1)).0" ;; \
	  patch) NEXT="$${MAJOR}.$${MINOR}.$$((PATCH+1))" ;; \
	  *) echo "TYPE must be patch, minor, or major"; exit 1 ;; \
	esac; \
	sed -i "s/^version: .*/version: $$NEXT/" deploy/helm/quorum/Chart.yaml; \
	git add deploy/helm/quorum/Chart.yaml; \
	git commit -m "chore(release): chart v$$NEXT"; \
	echo ""; \
	echo "Chart bumped to v$$NEXT — run: git push"
