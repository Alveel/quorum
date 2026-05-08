# Quorum

Internal tool for a ~15-person team to register leave and avoid coverage gaps. Each calendar day is color-coded by how many team members are present. Requesting a leave that would push any day below the configured minimum is hard-denied; an admin can override.

## User guide

### Heatmap

The home page shows a year calendar. Each day is colored:

| Color  | Meaning                                    |
|--------|--------------------------------------------|
| Green  | ≥75% of the team present                   |
| Yellow | ≥50% present                               |
| Orange | Above minimum, but close                   |
| Red    | At or below minimum — new requests blocked |

Days with admin-overridden leave show a hatched overlay. Hover over any day to see the date and exact count.

Navigate between years with the ← / → buttons.

### Registering leave

Fill in **From**, **To**, and an optional **Note**, then click **Register**. If any day in the range would drop below the minimum, the request is rejected and the offending dates are listed. Talk to your team or an admin about coverage before resubmitting.

### Cancelling leave

Your active leave appears below the registration form. Click **Cancel** next to any entry to remove it.

### Admin panel (`/admin`)

Admins (members of the configured OpenShift group) can:

- **Change settings** — minimum people present, team size, whether weekends count toward the threshold.
- **View all active leave** — all approved and overridden leave for the whole team.
- **Register an override leave** — bypass the threshold for a specific user. Requires a reason. Overridden leave still count toward the presence total; the heatmap shows them with a hatched overlay.

---

## Operations

### Prerequisites

- External PostgreSQL database (connection string only needed)
- OpenShift cluster with `oauth-proxy` sidecar capability
- Three existing Kubernetes Secrets (see below)

### Secrets to create before deploying

| Secret name (default) | Key                  | Content                                                                                |
|-----------------------|----------------------|----------------------------------------------------------------------------------------|
| `quorum-db`           | `DATABASE_URL`       | PostgreSQL connection string, e.g. `postgres://user:pass@host:5432/db?sslmode=require` |
| `quorum-cookie`       | `cookie-secret`      | 32 random bytes (base64), e.g. `openssl rand -base64 32`                               |
| `quorum-tls`          | `tls.crt`, `tls.key` | TLS certificate and key for the oauth-proxy HTTPS port                                 |

The chart references these secrets by name; it does not create them. This prevents secrets from appearing in rendered manifests or git history.

### Helm install

```sh
helm upgrade --install quorum deploy/helm/quorum \
  --set route.host=quorum.apps.cluster.example.com \
  --set admin.groups='{platform-team}' \
  --set team.size=15 \
  --set threshold.minPresentDefault=8
```

Key `values.yaml` knobs:

| Value                                | Default                                               | Description                    |
|--------------------------------------|-------------------------------------------------------|--------------------------------|
| `image.repository`                   | `ghcr.io/alveel/quorum`                               | Container image                |
| `image.tag`                          | `latest`                                              | Image tag                      |
| `database.secretName`                | `quorum-db`                                           | Secret holding the DB URL      |
| `oauthProxy.image`                   | `registry.redhat.io/openshift4/ose-oauth-proxy:v4.15` | Sidecar image                  |
| `oauthProxy.cookieSecret.secretName` | `quorum-cookie`                                       | Cookie secret                  |
| `oauthProxy.tls.secretName`          | `quorum-tls`                                          | TLS secret                     |
| `route.host`                         | _(required)_                                          | Public hostname                |
| `admin.groups`                       | `[]`                                                  | OpenShift groups granted admin |
| `team.size`                          | `15`                                                  | Default team size              |
| `threshold.minPresentDefault`        | `8`                                                   | Default minimum present        |

Preview manifests without installing:

```sh
helm template deploy/helm/quorum --values deploy/helm/quorum/values.yaml
```

### Environment variables (app container)

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `PORT` | No | `8080` | HTTP listen port |
| `ADMIN_GROUPS` | No | — | Comma-separated OpenShift group names granted admin |
| `TEAM_SIZE` | No | `15` | Initial team size (overridden by DB setting once changed) |
| `MIN_PRESENT_DEFAULT` | No | `8` | Initial minimum present threshold |
| `DEV_AUTH_BYPASS` | No | `false` | Must be `true` to enable dev bypass; never set in production |
| `DEV_USER` | No | — | Username to inject in dev mode |
| `DEV_ADMIN` | No | `false` | Grant admin in dev mode |

### Migrations

Migrations run automatically on startup before serving traffic. No separate Job is needed. The current schema creates: `users`, `absence`, `settings`, `audit_log`.

### Health probes

- `GET /healthz` — liveness (200 OK)
- `GET /readyz` — readiness (200 OK)

### Auth

Auth is handled entirely by the `oauth-proxy` sidecar. The Go app trusts `X-Forwarded-User`, `X-Forwarded-Email`, and `X-Forwarded-Groups` headers. These headers are set by the proxy; the pod's network policy ensures only the sidecar can reach the app port.

Admin access: a user is admin iff at least one of their groups (from `X-Forwarded-Groups`) matches a group in `ADMIN_GROUPS`. There is no per-user admin flag.

#### Migrating to Keycloak (no app changes required)

Two supported paths:
1. **Federate Keycloak into the cluster OAuth server** — no chart or app changes.
2. **Swap the sidecar** — replace `openshift/oauth-proxy` with upstream `oauth2-proxy` pointed at Keycloak's discovery URL. Change the `oauthProxy.image` and args in `values.yaml` only.

---

## Development

### Requirements

- Go 1.22+
- Docker / Podman with Compose
- [`templ`](https://templ.guide/) CLI (`go install github.com/a-h/templ/cmd/templ@latest`)
- `golangci-lint`
- `golang-migrate` CLI (for running migrations manually)

### Local dev setup

```sh
make dev
```

Starts Postgres via Docker Compose and the app on `:8080`. Auth is bypassed via `DEV_AUTH_BYPASS=true` and `DEV_USER` set in the Compose file. Set `DEV_ADMIN=true` to get admin access locally.

The app is available at `http://localhost:8080`.

### Make targets

| Target | What it does |
|---|---|
| `make dev` | Start Postgres + app (hot-reload not included) |
| `make build` | Compile static binary to `./bin/server` |
| `make test` | `go test ./...` |
| `make lint` | `golangci-lint run` |
| `make templ` | Regenerate Go from `*.templ` files |
| `make migrate` | Apply pending migrations against `$DATABASE_URL` |
| `make image` | Build the Docker image |

Run a single test:

```sh
go test ./internal/absence -run TestThreshold_DeniesWhenBelowMin
```

### Project layout

```
cmd/server/          main; wires config, store, server
internal/
  absence/           domain types, present() and threshold logic
  auth/              header parsing, admin check, dev bypass
  config/            env-driven config loading
  server/            chi router, middleware, HTTP handlers
  store/             PostgreSQL queries
  view/              templ components (heatmap, forms, admin)
migrations/          numbered SQL files, embedded in binary
web/static/          htmx.min.js, pico.min.css, app.css
deploy/helm/         Helm chart (app + oauth-proxy sidecar)
Dockerfile           multi-stage build
```

### Templating

Views use [`templ`](https://templ.guide/) — typed Go templates compiled to Go. After editing any `*.templ` file, run `make templ` before building or testing.

### Key invariant: one `Present()` function

`absence.Present()` in `internal/absence/threshold.go` is used by both the threshold check (deny/allow) and the heatmap coloring. Do not duplicate or diverge this logic — users would see green days that are actually blocked, or vice versa.

### Threshold logic

```
present(d) = team_size − count(approved + overridden absence covering d)
```

When evaluating a new request: subtract one more for the requester. If `present(d) < min_present` for any day `d` in the range → deny and return offending dates.

Weekend days are excluded from the check when `weekend_counts = false` (the default).

Admin override skips the threshold check, sets `status = 'overridden'`, and writes an `audit_log` row. Overridden leave still counts toward `present()`.

### CI

`.github/workflows/ci.yaml` runs `make test` and `make lint` on every push and pull request.
