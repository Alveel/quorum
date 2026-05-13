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

- External PostgreSQL database
- OpenShift 4.18+ **or** Kubernetes 1.33+ with GatewayAPI
- Secrets pre-created in the target namespace (see below)

### Secrets to create before deploying

The chart references secrets by name; it never creates them. Default names follow the `quorum-db-*` / `quorum-oauth-*` convention — rename via `values.yaml` if they clash with existing resources.

| Secret (default name) | Key             | Required when            | Content                                                  |
|-----------------------|-----------------|--------------------------|----------------------------------------------------------|
| `quorum-db-url`       | `DATABASE_URL`  | Always                   | PostgreSQL connection string                             |
| `quorum-oauth-cookie` | `cookie-secret` | Always                   | 32-byte random key (base64url) — encrypts session cookie |
| `quorum-oauth-oidc`   | `client-secret` | `proxy.provider: oidc`   | OIDC client secret                                       |
| `quorum-db-tls-ca`    | `ca.crt`        | `database.tlsCA.enabled` | CA certificate for Postgres TLS verification             |

Generate the cookie secret:
```sh
openssl rand -base64 32 | tr '+/' '-_' | head -c 32
```

No TLS secret is needed for the proxy — it runs plain HTTP on port 4180. TLS is terminated at the ingress layer (Route `edge` or Gateway).

### Helm install

**OpenShift — Route + openshift-oauth-proxy (default):**

```sh
helm upgrade --install quorum deploy/helm/quorum \
  --set admin.groups='{platform-team}' \
  --set team.size=15 \
  --set threshold.minPresentDefault=8
# OpenShift assigns a hostname automatically; set ingress.host to override.
```

**Kubernetes — GatewayAPI HTTPRoute + oauth2-proxy OIDC:**

```sh
helm upgrade --install quorum deploy/helm/quorum \
  --set ingress.type=httproute \
  --set ingress.host=quorum.example.com \
  --set ingress.gatewayRef.name=default \
  --set ingress.gatewayRef.namespace=gateway-system \
  --set proxy.provider=oidc \
  --set proxy.oidc.issuerURL=https://keycloak.example.com/realms/main \
  --set proxy.oidc.clientID=quorum \
  --set proxy.oidc.clientSecret.secretName=quorum-oauth-oidc \
  --set admin.groups='{quorum-admins}'
```

Key `values.yaml` knobs:

| Value                           | Default                                     | Description                                                         |
|---------------------------------|---------------------------------------------|---------------------------------------------------------------------|
| `image.tag`                     | Chart.AppVersion                            | Image tag (empty = chart default)                                   |
| `ingress.type`                  | `route`                                     | `route` (OpenShift) or `httproute` (k8s)                            |
| `ingress.host`                  | `""`                                        | Hostname; required for httproute                                    |
| `ingress.gatewayRef.name`       | `""`                                        | Gateway name (httproute only)                                       |
| `ingress.gatewayRef.namespace`  | `""`                                        | Gateway namespace (httproute only)                                  |
| `proxy.provider`                | `openshift`                                 | `openshift` or `oidc`                                               |
| `proxy.openshift.image`         | `quay.io/openshift/origin-oauth-proxy:4.21` | openshift-oauth-proxy image (see note below)                        |
| `proxy.oidc.issuerURL`          | `""`                                        | OIDC discovery URL (oidc only)                                      |
| `proxy.oidc.clientID`           | `""`                                        | OIDC client ID (oidc only)                                          |
| `proxy.oidc.emailDomain`        | `*`                                         | Allowed email domain(s) (oidc only)                                 |
| `proxy.cookieSecret.secretName` | `quorum-oauth-cookie`                       | Cookie encryption secret                                            |
| `database.secretName`           | `quorum-db-url`                             | Secret holding `DATABASE_URL`                                       |
| `database.tlsCA.enabled`        | `false`                                     | Mount CA cert for Postgres TLS                                      |
| `database.tlsCA.secretName`     | `""`                                        | Secret source — mutually exclusive with `configMapName`             |
| `database.tlsCA.configMapName`  | `""`                                        | ConfigMap source (e.g. cert-manager trust bundle)                   |
| `database.tlsCA.key`            | `ca.crt`                                    | Key within the Secret/ConfigMap (`ca-bundle.crt` for trust-manager) |
| `admin.groups`                  | `[]`                                        | Groups granted admin access                                         |
| `team.size`                     | `15`                                        | Default team size                                                   |
| `threshold.minPresentDefault`   | `8`                                         | Default minimum present                                             |

**Image note:** The default `quay.io/openshift/origin-oauth-proxy` is the OKD community build — publicly available, no pull secret needed. Red Hat ships a licensed build at `registry.redhat.io/openshift4/ose-oauth-proxy` (latest: `v4.14`) which requires a pull secret; see the [Red Hat Ecosystem Catalog](https://catalog.redhat.com/en/software/containers/openshift4/ose-oauth-proxy/5cdb2133bed8bd5717d5ae64) for available versions.

Preview manifests without installing:

```sh
helm template quorum deploy/helm/quorum
```

### Environment variables (app container)

| Variable              | Required | Default | Description                                                  |
|-----------------------|----------|---------|--------------------------------------------------------------|
| `DATABASE_URL`        | Yes      | —       | PostgreSQL connection string                                 |
| `PORT`                | No       | `8080`  | HTTP listen port                                             |
| `ADMIN_GROUPS`        | No       | —       | Comma-separated OpenShift group names granted admin          |
| `TEAM_SIZE`           | No       | `15`    | Initial team size (overridden by DB setting once changed)    |
| `MIN_PRESENT_DEFAULT` | No       | `8`     | Initial minimum present threshold                            |
| `DEV_AUTH_BYPASS`     | No       | `false` | Must be `true` to enable dev bypass; never set in production |
| `DEV_USER`            | No       | —       | Username to inject in dev mode                               |
| `DEV_ADMIN`           | No       | `false` | Grant admin in dev mode                                      |

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
2. **Switch to OIDC provider** — set `proxy.provider: oidc` and configure `proxy.oidc.*` in `values.yaml`. The app is unaffected; only the sidecar changes.

---

## Development

### Requirements

- Go 1.22+
- Podman with Compose
- [`templ`](https://templ.guide/) CLI (`go install github.com/a-h/templ/cmd/templ@latest`)
- `golangci-lint`
- `golang-migrate` CLI (for running migrations manually)

### Local dev setup

```sh
make dev
```

Starts Postgres via `podman-compose` and the app on `:8080`. Auth is bypassed via `DEV_AUTH_BYPASS=true` and `DEV_USER` set in the Compose file. Set `DEV_ADMIN=true` to get admin access locally.

The app is available at `http://localhost:8080`.

### Make targets

| Target                  | What it does                                      |
|-------------------------|---------------------------------------------------|
| `make dev`              | Start Postgres + app (hot-reload not included)    |
| `make build`            | Compile static binary to `./bin/server`           |
| `make test`             | `go test ./...`                                   |
| `make test-integration` | `go test -tags=integration ./internal/store/...`  |
| `make lint`             | `golangci-lint run`                               |
| `make templ`            | Regenerate Go from `*.templ` files                |
| `make migrate`          | Apply pending migrations against `$DATABASE_URL`  |
| `make image`            | Build container image from `Containerfile`        |
| `make helm-lint`        | Lint Helm chart against both scenario value files |

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
  locale/            locale files
  server/            chi router, middleware, HTTP handlers
  store/             PostgreSQL queries
  view/              templ components (heatmap, forms, admin)
migrations/          numbered SQL files, embedded in binary
web/static/          htmx.min.js, pico.min.css, app.css
deploy/helm/         Helm chart (app + oauth-proxy sidecar)
Containerfile        multi-stage build
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

`.github/workflows/ci.yaml` jobs on every push/PR:

| Job           | What it does                                                                            |
|---------------|-----------------------------------------------------------------------------------------|
| `test`        | `make test` + `make build`; checks templ is up to date                                  |
| `lint`        | `golangci-lint` via `make lint`                                                         |
| `helm-lint`   | `make helm-lint` (both ingress/proxy scenarios)                                         |
| `build-image` | Builds container image with `buildah`; pushes to `ghcr.io/alveel/quorum` on `main` only |

Dependency updates are automated via Renovate (`renovate.json`).
