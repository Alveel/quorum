# Vacation Coverage Tool — Implementation Plan

## Context
The team (~15 people) experiences low coverage during periods of overlapping vacations. We're building a small internal web app where team members register vacations and see, at a glance, which days are dangerously thin. Each day is colored on a green → yellow → orange → red gradient driven by how many people are present. Below an admin-configured **absolute minimum present per day**, a registration is **hard-denied** and surfaced for team discussion; an **admin** can override. Auth is delegated to an OpenShift `oauth-proxy` sidecar today, with a documented migration path to Keycloak (no app changes required). PostgreSQL is provided externally — we only consume a connection string.

The repository is currently empty; this plan covers the full bootstrap.

## Locked decisions
| Area | Choice |
|---|---|
| Backend | **Go** (chi router) |
| Templating | **templ** (typed Go templates) |
| Frontend | **HTMX** + minimal CSS (PicoCSS or hand-rolled) — no JS build step |
| DB | **External PostgreSQL** via connection string env var |
| Migrations | `golang-migrate`, embedded SQL files, applied on startup |
| Auth (now) | **OpenShift `oauth-proxy` sidecar**; app trusts `X-Forwarded-User` / `X-Forwarded-Email` / `X-Forwarded-Groups` |
| Auth (later) | Keycloak — either federated into the cluster OAuth, or swap sidecar for upstream `oauth2-proxy`. App contract unchanged |
| Roles | Regular user vs **admin**; admin determined by group claim (configurable via `ADMIN_GROUPS` env) |
| Threshold | **Absolute minimum people present** per workday, configurable by admin at runtime |
| Denial | **Hard deny** when crossing threshold, with admin override |
| Granularity | Whole days; primary view is a **year heatmap** |
| Deployment | **Helm chart** for OpenShift |

## Architecture
```
Browser ──► OpenShift Route (TLS edge)
              │
              ▼
        oauth-proxy sidecar  ──► OpenShift OAuth (today)
              │                  Keycloak (later, no app change)
              ▼
        Go app  :8080
              │
              ▼
        PostgreSQL (external; connection string only)
```
Single Go binary. The sidecar handles login + cookie session; the app reads identity from forwarded headers and trusts them (the pod's network policy ensures only the sidecar reaches the app port).

## Repository layout (to create)
```
cmd/server/main.go
internal/
  config/         env-driven config
  auth/           parse forwarded headers, admin-group check, dev bypass
  vacation/       domain types + threshold/coverage logic
  store/          Postgres queries (hand-written sqlx or sqlc)
  server/         chi router, middleware, handlers
  view/           templ components (heatmap, forms, admin pages)
migrations/       0001_init.sql, ...
web/static/       htmx.min.js, pico.min.css
deploy/helm/vacation-coverage/
  Chart.yaml
  values.yaml
  templates/
    deployment.yaml      # app + oauth-proxy sidecar
    service.yaml
    route.yaml
    serviceaccount.yaml  # with serviceaccounts.openshift.io/oauth-redirectreference annotation
    configmap.yaml
    secret.yaml          # references existing secrets, does not create them
Dockerfile               # multi-stage; runtime on ubi9-micro or distroless
docker-compose.yaml      # local dev: app + postgres, oauth-proxy bypassed
Makefile                 # dev / test / lint / templ generate / migrate
.github/workflows/ci.yaml
CLAUDE.md
README.md
```

## Data model (initial migration)
- `users(id text pk, email text, display_name text, is_admin bool, created_at timestamptz)` — `id` is the forwarded OpenShift username; rows are upserted on first login
- `vacations(id uuid pk, user_id text fk, start_date date, end_date date, note text, status text check in ('approved','overridden','cancelled'), created_at timestamptz, created_by text)`
- `settings(key text pk, value jsonb, updated_at, updated_by)` — holds `min_present`, `team_size`, `weekend_counts` (bool)
- `audit_log(id uuid pk, actor_id text, action text, target_id text, payload jsonb, at timestamptz)` — used at minimum for overrides and settings changes

## Threshold logic (`internal/vacation`)
1. Resolve `team_size` (from settings, defaults to count of distinct `users`).
2. For each calendar day `d` in the requested range:
   - `present(d) = team_size − count(approved/overridden vacations covering d, excluding the request being evaluated)`
   - If a candidate request is being evaluated, subtract one more for that user.
3. If any `d` has `present(d) < min_present` → reject and return the list of offending dates so the UI can highlight them.
4. Admin override path: same query, status set to `overridden`, audit_log entry written.

Heatmap coloring uses the same `present(d)` function:
- `present ≤ min_present` → red (also: blocked)
- `min_present < present ≤ min_present + 1` → orange
- Middle band → yellow
- Near full team → green

Continuous interpolation; admin-overridden vacations rendered with a hatched/striped overlay so they're visible without changing the count.

## HTTP surface (HTMX-friendly, server-rendered)
- `GET /` — year heatmap, summary
- `GET /me` — own vacations, cancel button
- `POST /vacations` — create; returns either an HTMX OOB-swap of the heatmap, or a 422 partial with the offending dates
- `DELETE /vacations/{id}` — cancel own vacation
- `POST /admin/override` — admin-only; creates a vacation bypassing the threshold
- `GET /admin` / `POST /admin/settings` — admin-only; edit `min_present`, `team_size`, etc.
- `GET /healthz`, `GET /readyz` — for OpenShift probes
- `GET /static/*` — embedded assets via `embed.FS`

## Helm chart (`deploy/helm/vacation-coverage`)
Templates:
- **Deployment**: 1 replica, two containers
  - `app`: env from ConfigMap + Secret (`DATABASE_URL`, `MIN_PRESENT_DEFAULT`, `ADMIN_GROUPS`, `TEAM_SIZE`)
  - `oauth-proxy`: OpenShift image, mounts cookie-secret + tls secrets, `--upstream=http://localhost:8080`, `--pass-user-headers`, `--pass-access-token=false`, group filter via `--openshift-group=<group>` if desired
- **Service**: ClusterIP, exposes the proxy port (`8443`)
- **Route**: edge or reencrypt termination
- **ServiceAccount** with `serviceaccounts.openshift.io/oauth-redirectreference.primary` annotation pointing at the Route — this is what makes the SA usable as an OAuth client
- **Secret**: references existing secrets (DB URL, cookie secret) — chart does not generate them; values point at secret name + key
- **ConfigMap**: non-sensitive defaults

`values.yaml` knobs (initial):
```yaml
image: { repository, tag, pullPolicy }
database: { secretName, secretKey }   # connection string lives here
oauthProxy:
  image: registry.redhat.io/openshift4/ose-oauth-proxy:...
  cookieSecret: { secretName, secretKey }
  tls: { secretName }
route: { host, tls: edge }
admin: { groups: [] }                 # OpenShift groups granted admin
team: { size: 15 }
threshold: { minPresentDefault: 8 }
resources: { app: ..., oauthProxy: ... }
```

## Local dev
- `docker-compose up` brings up Postgres + the app on :8080
- oauth-proxy is **not** run locally; instead the app honors a `DEV_USER` (and optional `DEV_ADMIN=true`) env var that injects the same forwarded-header values. Off in production by an explicit `DEV_AUTH_BYPASS=true` guard
- `make migrate` / `make dev` / `make test` / `make templ`

## Verification
- `go test ./...` — unit tests for threshold math (single-day, range, edge of min, override path), header parsing, admin gating
- `docker compose up` then `curl localhost:8080/?... ` and a browser walkthrough of: register vacation that fits, register one that crosses threshold (expect 422 + offending dates), admin override path, settings change
- `helm template deploy/helm/vacation-coverage --values values.yaml` — eyeball the manifests for: SA redirect annotation, oauth-proxy args, route TLS, secret references
- `helm install` into a dev OpenShift namespace, log in via OpenShift, register a vacation, confirm the heatmap updates, change `min_present`, see denial fire, exercise admin override end-to-end
- (Later) verify the Keycloak migration path by federating Keycloak into the cluster OAuth in a dev cluster — should be invisible to the app

## Out of scope (explicit)
- Postgres provisioning, backups, HA — externally managed
- Email/Slack notifications on denial — can be added later via an outbox table
- Mobile app, ICS feed, sync to corporate calendar — future
- Half-day vacations — future (would change schema and threshold math)
