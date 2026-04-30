# CLAUDE.md

Guidance to Claude Code (claude.ai/code) for this repo.

## What this is
Small internal tool, ~15-person team. Register vacations, avoid coverage gaps. Each calendar day colored by people-present count (green → yellow → orange → red). Registration pushing any day below admin-configured **minimum present** = **hard-denied**; **admin** can override.

## Stack at a glance
- **Go** + **chi** + **templ** + **HTMX** — server-rendered, no JS build pipeline
- **PostgreSQL**, external (connection string only); migrations via `golang-migrate`, embedded SQL
- **OpenShift `oauth-proxy` sidecar** in front of app; Go process never authenticates users

## Common commands
```sh
make dev          # runs postgres via docker-compose + the app on :8080 (DEV_USER bypass)
make test         # go test ./...
make lint         # golangci-lint run
make templ        # regenerate templ files (run after editing *.templ)
make migrate      # apply migrations against $DATABASE_URL
make build        # static binary into ./bin/server
make image        # docker build of the runtime image
helm template deploy/helm/vacation-coverage   # render manifests for review
```
Single test: `go test ./internal/vacation -run TestThreshold_DeniesWhenBelowMin`.

## Architecture notes that aren't obvious from the code

### Auth is a header contract, not a library
App trusts `X-Forwarded-User`, `X-Forwarded-Email`, `X-Forwarded-Groups` set by `oauth-proxy` sidecar. Pod's only ingress = proxy, so headers authoritative. **Do not** add OIDC/OAuth code into Go app — auth changes (e.g. switching to Keycloak) done by reconfiguring/replacing sidecar. Two supported migration paths, both header-compatible:
1. Federate Keycloak as OIDC identity provider in cluster's OAuth server (no chart change to app).
2. Replace `openshift/oauth-proxy` with upstream `oauth2-proxy` pointed at Keycloak's discovery URL (Helm values change only).

Local dev: `DEV_AUTH_BYPASS=true` + `DEV_USER=<name>` + optional `DEV_ADMIN=true` synthesize same headers. Bypass refuses to activate unless `DEV_AUTH_BYPASS` explicitly set.

Admin role = group claim: user is admin iff any `X-Forwarded-Groups` value is in comma-separated `ADMIN_GROUPS` env var. No per-user admin flag in app — group membership is source of truth.

### One `present(d)` function, two consumers
Threshold check (denial) and heatmap coloring **must** use same `present(d int) (count int)` in `internal/vacation`. Divergence = users see "green" days that are actually blocked, or vice versa. Keep single shared function.

### Overrides change status, not counts
Admin overrides set `vacations.status = 'overridden'`, write `audit_log` row. Overridden vacations **do** count against `present(d)` like any approved vacation — override only bypassed *creation* check. UI shows hatched overlay so team spots intentionally-thin days.

### Migrations on startup
App applies pending migrations on boot before serving traffic. Don't run separate `Job`; chart relies on single Deployment. If migrations need gating (e.g. destructive change), introduce separate sub-command before adding job infrastructure.

## Layout
- `cmd/server/` — main; wires config, store, server
- `internal/config` — env loading
- `internal/auth` — header parsing, admin check, dev bypass
- `internal/vacation` — domain types and single `present(d)` / threshold logic
- `internal/store` — Postgres queries
- `internal/server` — chi router, middleware, handlers
- `internal/view` — templ components (heatmap, forms, admin)
- `migrations/` — numbered SQL files, embedded
- `web/static/` — htmx, css
- `deploy/helm/vacation-coverage/` — chart with app + oauth-proxy sidecar

## Deployment notes
- `ServiceAccount` carries `serviceaccounts.openshift.io/oauth-redirectreference.primary` pointing at Route — without it, SA can't act as OAuth client.
- Chart **references** existing secrets (DB URL, cookie secret, TLS) by name; doesn't generate them. Avoids leaking secrets into git or rendered manifests.
- Single replica fine; Postgres external, no in-process state worth replicating for 15 users.