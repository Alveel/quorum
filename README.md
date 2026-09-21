# Quorum

Internal tool for a ~15-person team to register leave and avoid coverage gaps. Each member has a weekly working-day pattern and holds one or more admin-defined **roles**, each with its own minimum-present quota; a global minimum applies on top. Each calendar day is color-coded by the worst of the global and per-role coverage ratios. Requesting leave that would push any day below any applicable minimum is hard-denied; an admin can override.

## User guide

### Heatmap

The home page shows a year calendar. Each day is colored:

| Color   | Meaning                                                      |
|---------|--------------------------------------------------------------|
| Green   | Comfortable headroom — ≥75% of the way from minimum to fully staffed |
| Yellow  | ≥50% of that headroom                                        |
| Orange  | Above minimum, but close                                     |
| Red     | At or below a minimum — new requests blocked                 |
| Neutral | Nobody scheduled: a public holiday, or a day outside everyone's pattern |

The ratio is measured against the *minimum*, not against zero: a day sits at green when present is at least 75% of the way from the minimum up to the number of people scheduled. A day takes the worst color of the global count and each role's count, so a day can show red because one role is short-staffed even though overall attendance looks fine — open the day for the per-role breakdown.

Days with admin-overridden leave show a hatched overlay. Hover over any day to see the date and exact count; click it for a per-role panel.

Navigate between years with the ← / → buttons.

### Registering leave

First set your working days and role(s) under [Settings](#member-settings-settings) — until you do, the registration form is hidden, because coverage can't be computed for someone with no pattern and no role.

Fill in **From**, **To**, and an optional **Note**, then click **Register**. If any day in the range would drop the global count or one of *your own* roles below its minimum, the request is rejected and the offending dates are listed. Days you don't work, and public holidays, are never offending. Talk to your team or an admin about coverage before resubmitting.

### Cancelling leave

Your active leave appears below the registration form. Click **Cancel** next to any entry to remove it.

### Admin panel (`/admin`)

Admins (members of the configured OpenShift group) can:

- **Change settings** — the global minimum present, and the country whose public holidays are synced.
- **Manage the roster** — add members, set display name and active flag, and edit any member's working days and roles.
- **Manage roles** — create, rename and delete roles, each with its own minimum-present quota. A role whose scheduled headcount already falls short of its quota within the next ~180 days is flagged, before anyone books leave.
- **Manage public holidays** — sync from an offline calendar for the configured country, or add and remove dates by hand.
- **View all active leave** — all approved and overridden leave for the whole team.
- **Register an override leave** — bypass the coverage check for a specific user. Requires a reason. Overridden leave still counts toward the presence total; the heatmap shows it with a hatched overlay.

### Member settings (`/settings`)

Every member sets their own **working days** (which weekdays they normally work) and their **roles**. Both feed coverage directly: you are only counted as expected-present on days your pattern covers, and each role you hold counts you toward that role's quota. A member with no roles cannot register leave until they pick at least one.

Admins can edit any member's settings from the roster.

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
| `proxy.openshift.image`         | `quay.io/openshift/origin-oauth-proxy:4.22` | openshift-oauth-proxy image (see note below)                        |
| `proxy.oidc.image`              | `quay.io/oauth2-proxy/oauth2-proxy:v7.15.4` | oauth2-proxy image (oidc only)                                      |
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
| `threshold.minPresentDefault`   | `8`                                         | Default global minimum present                                      |

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
| `MIN_PRESENT_DEFAULT` | No       | `8`     | Initial global minimum present threshold                     |
| `DEV_AUTH_BYPASS`     | No       | `false` | Must be `true` to enable dev bypass; never set in production |
| `DEV_USER`            | No       | —       | Username to inject in dev mode                               |
| `DEV_ADMIN`           | No       | `false` | Grant admin in dev mode                                      |

### Migrations

Migrations run automatically on startup before serving traffic. No separate Job is needed. The current schema creates `users`, `absence`, `settings`, `audit_log`, `roles`, `user_roles` and `holidays`.

Migration `000002` removed the `team_size` and `weekend_counts` settings: both are superseded by per-member working-day patterns and per-role minimums. The live settings keys are `min_present` and `holiday_country`.

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

- Go 1.26+
- Podman with Compose
- [`templ`](https://templ.guide/) CLI (`go install github.com/a-h/templ/cmd/templ@latest`)
- `golangci-lint`
- `golang-migrate` CLI (for running migrations manually)

### Local dev setup

```sh
make dev
```

Starts Postgres via `podman-compose` and the app on `:8080`. The `dev` target sets `DEV_AUTH_BYPASS=true`, `DEV_USER=devuser` and `DEV_ADMIN=true` for you; the Compose file only runs the database. The bypass refuses to activate unless `DEV_AUTH_BYPASS` is explicitly set.

The app is available at `http://localhost:8080`.

### Make targets

| Target                  | What it does                                                                        |
|-------------------------|-------------------------------------------------------------------------------------|
| `make dev`              | Start Postgres + app (hot-reload not included)                                      |
| `make build`            | Compile static binary to `./bin/server`                                             |
| `make test`             | `go test ./...`                                                                     |
| `make test-integration` | `go test -tags=integration ./internal/store/...`                                    |
| `make lint`             | `golangci-lint run`                                                                 |
| `make templ`            | Regenerate Go from `*.templ` files                                                  |
| `make migrate`          | Apply pending migrations against `$DATABASE_URL`                                    |
| `make image`            | Build container image from `Containerfile`                                          |
| `make helm-lint`        | Lint Helm chart against both scenario value files                                   |
| `make release`          | Bump `appVersion` + chart `version`, commit, tag, push (`TYPE=patch\|minor\|major`) |
| `make release-chart`    | Bump chart `version` only (chart-only changes), commit, push                        |

Run a single test:

```sh
go test ./internal/absence -run TestCoverage_ColorBoundaries_MatchOldSemantics
```

Integration tests need a live database and are behind a build tag, so `make test` does not run them:

```sh
make test-integration    # truncates the dev database
```

### Project layout

```
cmd/server/          main; wires config, store, server
internal/
  absence/           domain types + Coverage(); no dependencies outside stdlib
  auth/              header parsing, admin check, dev bypass
  config/            env-driven config loading
  coverage/          loads Coverage()'s inputs and computes it for a date range
  holidaysync/       offline public-holiday calendar sync (rickar/cal)
  locale/            en/nl messages, date formatting (go-i18n)
  server/            chi router, middleware, HTTP handlers
  store/             PostgreSQL queries
  view/              templ components (heatmap, forms, admin, roster, roles,
                     holidays, member settings)
migrations/          numbered SQL files, embedded in binary
web/static/          htmx.min.js, pico.min.css, app.css
deploy/helm/         Helm chart (app + oauth-proxy sidecar)
Containerfile        multi-stage build
```

### Templating

Views use [`templ`](https://templ.guide/) — typed Go templates compiled to Go. After editing any `*.templ` file, run `make templ` before building or testing.

### Key invariant: one `Coverage()` function

`absence.Coverage()` in `internal/absence/coverage.go` is the single source of per-day presence math. Three consumers read from it — heatmap coloring, the request denial check (`OffendingDays`), and the admin role-feasibility warning (`FeasibilityWarnings`). Do not compute presence counts anywhere else: divergence means users see green days that are actually blocked, or vice versa.

`internal/coverage` is the seam in front of it. It loads every input — settings, roster, roles, holidays, absences — and returns a snapshot for a date range, so handlers ask for a range instead of assembling the arguments themselves.

### Coverage logic

For each day in range:

```
expected(d) = active members whose working-day pattern covers d
present(d)  = expected(d) − those with approved/overridden leave covering d
```

The same is computed per role, counting only members holding that role. A day is colored by the worst of the global ratio and every role's ratio. A day where `expected(d) == 0` — a public holiday, or a day nobody's pattern covers — is neutral, never red.

When evaluating a new request the candidate is counted before it exists. It is denied if any day drops the global count, or one of the requester's own roles, **strictly below** its minimum.

Note the deliberate asymmetry: a count sitting *exactly* at its minimum renders red (`<=`) but is still approvable (`<`). Red means "no more room", not "already broken".

Admin override skips the denial check, sets `status = 'overridden'`, and writes an `audit_log` row. Overridden leave still counts against coverage like any approved leave.

### CI

The repository is mirrored, and both pipelines run the same checks:

- **GitLab CI** — `.gitlab-ci.yml`
- **GitHub Actions** — `.github/workflows/ci.yaml`

| Job             | What it does                                                                            |
|-----------------|-----------------------------------------------------------------------------------------|
| `commit-lint`   | Conventional Commits check                                                              |
| `test`          | `make test` + `make build`; checks templ is up to date                                  |
| `lint`          | `golangci-lint` via `make lint`                                                         |
| `helm-lint`     | `make helm-lint` (both ingress/proxy scenarios)                                         |
| `build-image`   | Builds container image with `buildah`; pushes on `main` only                            |
| `publish-chart` | Publishes the Helm chart                                                                |

**Integration tests do not run in either pipeline.** `make test` skips them via the `integration` build tag, and no job runs `make test-integration`. Changes to `internal/store` are only covered if someone runs it locally against a database.

Dependency updates are automated via Renovate (`renovate.json`).
