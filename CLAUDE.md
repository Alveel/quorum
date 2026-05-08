# CLAUDE.md

Guidance to Claude Code (claude.ai/code) for this repo.

## What this is
Small internal tool, ~15-person team. Register leave, avoid coverage gaps. Each calendar day colored by people-present count (green → yellow → orange → red). Registration pushing any day below admin-configured **minimum present** = **hard-denied**; **admin** can override.

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
helm template deploy/helm/quorum   # render manifests for review
```
Single test: `go test ./internal/leave -run TestThreshold_DeniesWhenBelowMin`.

## Architecture notes that aren't obvious from the code

### Auth is a header contract, not a library
App trusts `X-Forwarded-User`, `X-Forwarded-Email`, `X-Forwarded-Groups` set by `oauth-proxy` sidecar. Pod's only ingress = proxy, so headers authoritative. **Do not** add OIDC/OAuth code into Go app — auth changes (e.g. switching to Keycloak) done by reconfiguring/replacing sidecar. Two supported migration paths, both header-compatible:
1. Federate Keycloak as OIDC identity provider in cluster's OAuth server (no chart change to app).
2. Replace `openshift/oauth-proxy` with upstream `oauth2-proxy` pointed at Keycloak's discovery URL (Helm values change only).

Local dev: `DEV_AUTH_BYPASS=true` + `DEV_USER=<name>` + optional `DEV_ADMIN=true` synthesize same headers. Bypass refuses to activate unless `DEV_AUTH_BYPASS` explicitly set.

Admin role = group claim: user is admin iff any `X-Forwarded-Groups` value is in comma-separated `ADMIN_GROUPS` env var. No per-user admin flag in app — group membership is source of truth.

### One `present(d)` function, two consumers
Threshold check (denial) and heatmap coloring **must** use same `present(d int) (count int)` in `internal/leave`. Divergence = users see "green" days that are actually blocked, or vice versa. Keep single shared function.

### Overrides change status, not counts
Admin overrides set `leave.status = 'overridden'`, write `audit_log` row. Overridden leave **do** count against `present(d)` like any approved leave — override only bypassed *creation* check. UI shows hatched overlay so team spots intentionally-thin days.

### Migrations on startup
App applies pending migrations on boot before serving traffic. Don't run separate `Job`; chart relies on single Deployment. If migrations need gating (e.g. destructive change), introduce separate sub-command before adding job infrastructure.

## Layout
- `cmd/server/` — main; wires config, store, server
- `internal/config` — env loading
- `internal/auth` — header parsing, admin check, dev bypass
- `internal/leave` — domain types and single `present(d)` / threshold logic
- `internal/store` — Postgres queries
- `internal/server` — chi router, middleware, handlers
- `internal/view` — templ components (heatmap, forms, admin)
- `migrations/` — numbered SQL files, embedded
- `web/static/` — htmx, css
- `deploy/helm/leave-coverage/` — chart with app + oauth-proxy sidecar

## Deployment notes
- `ServiceAccount` carries `serviceaccounts.openshift.io/oauth-redirectreference.primary` pointing at Route — without it, SA can't act as OAuth client.
- Chart **references** existing secrets (DB URL, cookie secret, TLS) by name; doesn't generate them. Avoids leaking secrets into git or rendered manifests.
- Single replica fine; Postgres external, no in-process state worth replicating for 15 users.

<!-- code-review-graph MCP tools -->
## MCP Tools: code-review-graph

**IMPORTANT: This project has a knowledge graph. ALWAYS use the
code-review-graph MCP tools BEFORE using Grep/Glob/Read to explore
the codebase.** The graph is faster, cheaper (fewer tokens), and gives
you structural context (callers, dependents, test coverage) that file
scanning cannot.

### When to use graph tools FIRST

- **Exploring code**: `semantic_search_nodes` or `query_graph` instead of Grep
- **Understanding impact**: `get_impact_radius` instead of manually tracing imports
- **Code review**: `detect_changes` + `get_review_context` instead of reading entire files
- **Finding relationships**: `query_graph` with callers_of/callees_of/imports_of/tests_for
- **Architecture questions**: `get_architecture_overview` + `list_communities`

Fall back to Grep/Glob/Read **only** when the graph doesn't cover what you need.

### Key Tools

| Tool                        | Use when                                               |
|-----------------------------|--------------------------------------------------------|
| `detect_changes`            | Reviewing code changes — gives risk-scored analysis    |
| `get_review_context`        | Need source snippets for review — token-efficient      |
| `get_impact_radius`         | Understanding blast radius of a change                 |
| `get_affected_flows`        | Finding which execution paths are impacted             |
| `query_graph`               | Tracing callers, callees, imports, tests, dependencies |
| `semantic_search_nodes`     | Finding functions/classes by name or keyword           |
| `get_architecture_overview` | Understanding high-level codebase structure            |
| `refactor_tool`             | Planning renames, finding dead code                    |

### Workflow

1. The graph auto-updates on file changes (via hooks).
2. Use `detect_changes` for code review.
3. Use `get_affected_flows` to understand impact.
4. Use `query_graph` pattern="tests_for" to check coverage.
