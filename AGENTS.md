# AGENTS.md

Guidance for coding agents working in this repo.

## What this is
Small internal tool, ~15-person team. Register leave, avoid coverage gaps. Each member has a weekly working-day pattern and holds one or more admin-defined **roles**, each with its own minimum-present quota; global minimum-present applies too. Each calendar day colored by worst-of (global vs every role) coverage ratio (green → yellow → orange → red), or a distinct "none" state on a day nobody's scheduled (holiday, or off-pattern). Registration pushing any day's global or role coverage strictly below its minimum = **hard-denied**; **admin** can override. A member with zero roles assigned is blocked from registering leave until they configure themselves via `/settings`.

## Stack at a glance
- **Go** + **chi** + **templ** + **HTMX** — server-rendered, no JS build pipeline
- **PostgreSQL**, external (connection string only); migrations via `golang-migrate`, embedded SQL
- **OpenShift `oauth-proxy` sidecar** in front of app; Go process never authenticates users

## Common commands
```sh
make dev          # runs postgres via podman-compose + the app on :8080 (DEV_USER bypass)
make test         # go test ./... — does NOT include integration tests (build tag)
make test-integration  # -tags=integration ./internal/store/... — needs a DB, TRUNCATES it
make lint         # golangci-lint run
make templ        # regenerate templ files (run after editing *.templ; commit the
                  # generated _templ.go alongside — CI checks freshness)
make migrate      # apply migrations against $DATABASE_URL
make build        # static binary into ./bin/server
make image        # podman build of the runtime image
helm template deploy/helm/quorum   # render manifests for review
```
Single test: `go test ./internal/absence -run TestCoverage_ColorBoundaries_MatchOldSemantics`.

## Architecture notes that aren't obvious from the code

### Auth is a header contract, not a library
App trusts `X-Forwarded-User`, `X-Forwarded-Email`, `X-Forwarded-Groups` set by `oauth-proxy` sidecar. Pod's only ingress = proxy, so headers authoritative. **Do not** add OIDC/OAuth code into Go app — auth changes (e.g. switching to Keycloak) done by reconfiguring/replacing sidecar. Two supported migration paths, both header-compatible:
1. Federate Keycloak as OIDC identity provider in cluster's OAuth server (no chart change to app).
2. Replace `openshift/oauth-proxy` with upstream `oauth2-proxy` pointed at Keycloak's discovery URL (Helm values change only).

Local dev: `DEV_AUTH_BYPASS=true` + `DEV_USER=<name>` + optional `DEV_ADMIN=true` synthesize same headers. Bypass refuses to activate unless `DEV_AUTH_BYPASS` explicitly set.

Admin role = group claim: user is admin iff any `X-Forwarded-Groups` value is in comma-separated `ADMIN_GROUPS` env var. No per-user admin flag in app — group membership is source of truth.

### One `Coverage()` function, three consumers
Heatmap coloring, the absence-request denial check (`OffendingDays`), and the admin role-feasibility warning (`FeasibilityWarnings`) **must** all read from the same `Coverage(roster, absences, holidays, roles, globalMinPresent, from, to) map[time.Time]DayCoverage` in `internal/absence`. Divergence = users see "green" days that are actually blocked, or vice versa. Keep a single shared function — don't recompute presence counts anywhere else.

Handlers don't call `Coverage` directly: `internal/coverage` loads every input (settings, roster, roles, holidays, absences) and returns a `Snapshot` for a date range. `ForRange(ctx, from, to, extra...)` also merges not-yet-persisted absences, which is how the denial check evaluates a candidate — `Snapshot.Absences` deliberately excludes them, so nothing renders an absence that isn't saved. Add new callers there, not by re-assembling `Coverage`'s arguments.

`Coverage` also encodes two asymmetries worth knowing: a role/global count at *exactly* its minimum renders red (`Failing`, `<=`) but is still approvable (`OffendingDays`, strict `<`) — matches the pre-role-quota threshold behavior. And a day nobody's scheduled on (holiday, or off everyone's weekly pattern) is `NoOneScheduled`/color `"none"`, never red — `Expected == 0` must not fall through to the red-at-zero path.

### Overrides change status, not counts
Admin overrides set `absence.status = 'overridden'`, write `audit_log` row. Overridden absences **do** count against `Coverage()` like any approved absence — override only bypassed *creation* check. UI shows hatched overlay so team spots intentionally-thin days.

### Migrations on startup
App applies pending migrations on boot before serving traffic. Don't run separate `Job`; chart relies on single Deployment. If migrations need gating (e.g. destructive change), introduce separate sub-command before adding job infrastructure.

### Failed loads are withheld, never rendered
A fragment whose data failed to load is **not** rendered from a zero value. A nil roster makes `Coverage` report every day as `NoOneScheduled`; a failed settings read drops the minimum to 0, which colors days more favourably than the truth. Both produce a confident-looking, wrong heatmap at HTTP 200. The out-of-band tails omit the fragment instead and render `view.RefreshNotice`, leaving the stale-but-consistent DOM in place. Keep that property when touching them.

Note `cancelAbsence` cannot simply omit: its button swaps `#my-absences` by `outerHTML`, so dropping the element removes the section and strands later cancels. It renders `view.MyAbsencesUnavailable` in that element instead.

## Traps that have already cost time

### Locale files are hand-formatted — never round-trip them
`internal/locale/locales/{en,nl}.json` are one line per key, `{ "other": ... }` column-aligned, grouped by blank lines. Loading and re-dumping via any JSON library reformats all ~80 entries and buries a 4-line change in a 300-line diff. Insert text directly, padding new keys to the widest in their group.

### A missing translation fails silently
`locale.T` returns the *message ID* when a lookup misses, rather than erroring. A key added to `en.json` and forgotten in `nl.json` renders as a raw identifier like `refresh_notice_link` on the Dutch site, with nothing failing and no test catching it. Always add to both.

### Integration tests are invisible to `make test` and to CI
They're behind `//go:build integration`, so `go test ./...` doesn't even compile them — a compile break there goes unnoticed. **Neither pipeline runs them**, so `internal/store` changes are only ever verified by someone running `make test-integration` locally. Check compilation with `go vet -tags=integration ./internal/store/...`.

Two further traps: `go test` caching can't see database state, so a `cached` result proves nothing — use `-count=1`. And `truncateAll` wipes the dev database, so ask before running.

## Layout
- `cmd/server/` — main; wires config, store, server
- `internal/config` — env loading
- `internal/auth` — header parsing, admin check, dev bypass
- `internal/absence` — domain types, roster/role/holiday types, single `Coverage()` function
- `internal/coverage` — loads `Coverage()`'s inputs and computes it for a date range; the seam handlers use
- `internal/holidaysync` — offline public-holiday calendar sync (rickar/cal), kept out of `internal/absence` so the domain package stays dependency-free
- `internal/store` — Postgres queries
- `internal/server` — chi router, middleware, handlers
- `internal/view` — templ components (heatmap, forms, admin, roster/roles/holidays, member settings)
- `internal/locale` — en/nl i18n (go-i18n), date formatting
- `migrations/` — numbered SQL files, embedded
- `web/static/` — htmx, css
- `deploy/helm/quorum/` — chart with app + oauth-proxy sidecar

## CI/CD
Two remotes, two pipelines, same checks. `origin` = GitLab (`git.nationaalarchief.net/akik/quorum`) — **canonical**, holds the issues and merge requests. `github` = mirror (`Alveel/quorum`).
- GitLab CI: `.gitlab-ci.yml` — `commit-lint`, `test`, `lint`, `helm-lint`, `build-image`, `publish-chart`, `renovate`
- GitHub Actions: `.github/workflows/ci.yaml` — same job set
- Image pushed to `ghcr.io/alveel/quorum` tagged `:latest` + `:<sha>`
- Renovate (`renovate.json`) tracks Go modules, Actions versions, Containerfile base images
- Neither pipeline runs `make test-integration` — see the trap above
- The two remotes drift; check which one a branch tracks before assuming a push reached the pipeline you mean

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

## Agent skills

### Issue tracker

Issues live in GitLab Issues on the `na` remote (`git.nationaalarchief.net/akik/quorum`), via the `glab` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary; label strings equal the role names. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
