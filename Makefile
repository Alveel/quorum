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

# Bump app + chart version together, commit, tag, and push.
# TYPE=patch|minor|major (default: patch).
release:
	@TYPE=$${TYPE:-patch}; \
	APP_CURRENT=$$(grep '^appVersion:' deploy/helm/quorum/Chart.yaml | awk '{print $$2}' | tr -d '"'); \
	CHART_CURRENT=$$(grep '^version:' deploy/helm/quorum/Chart.yaml | awk '{print $$2}'); \
	next_ver() { \
	  MAJOR=$$(echo $$1 | cut -d. -f1); \
	  MINOR=$$(echo $$1 | cut -d. -f2); \
	  PATCH=$$(echo $$1 | cut -d. -f3); \
	  case $$TYPE in \
	    major) echo "$$((MAJOR+1)).0.0" ;; \
	    minor) echo "$${MAJOR}.$$((MINOR+1)).0" ;; \
	    patch) echo "$${MAJOR}.$${MINOR}.$$((PATCH+1))" ;; \
	    *) echo "TYPE must be patch, minor, or major" >&2; exit 1 ;; \
	  esac; \
	}; \
	APP_NEXT=$$(next_ver $$APP_CURRENT); \
	CHART_NEXT=$$(next_ver $$CHART_CURRENT); \
	sed -i "s/^appVersion: .*/appVersion: \"$$APP_NEXT\"/" deploy/helm/quorum/Chart.yaml; \
	sed -i "s/^version: .*/version: $$CHART_NEXT/" deploy/helm/quorum/Chart.yaml; \
	git add deploy/helm/quorum/Chart.yaml; \
	git commit -m "chore(release): v$$APP_NEXT"; \
	git tag "v$$APP_NEXT"; \
	git push && git push --tags; \
	echo "Released v$$APP_NEXT (chart v$$CHART_NEXT)"

# Bump chart version only (chart template/values changes, no app change).
# TYPE=patch|minor|major (default: patch).
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
	git push; \
	echo "Chart released v$$NEXT"
