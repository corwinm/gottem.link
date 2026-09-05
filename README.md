# gottem.link

A small personal URL shortener written in Go with SQLite.

The public homepage at `/` explains the service and links to the private console at `/admin/`. It does not expose public link creation or a link directory. The dependency-free page is embedded in the Go binary and leaves existing redirect routes unchanged.

## Development

Requires Go 1.27, a C compiler for `go-sqlite3`, `sqlite3`, and `make`.

```sh
make tools
make check
make run
```

The local server listens on `:8080` and stores data in `./gottem.db`. Override either value directly:

```sh
go run . -addr :3000 -dsn /path/to/gottem.db
```

Set `GOTTEM_MANAGEMENT_TOKEN` to enable the private JSON management API. `GOTTEM_BACKUP_TOKEN` may separately grant read-only access to the export endpoint; it cannot access redirect management, imports, or browser sessions. Without either token, `/api/` returns 404. Creating a redirect accepts an optional `slug` and RFC3339 `expires_at`; omitted or `null` slugs are generated automatically, while custom slugs are validated and stored in lowercase. Expired and disabled links return 404 publicly but remain inspectable, and their slugs remain reserved. `destination_updated_at` records only the latest destination replacement; `updated_at` continues to record any lifecycle mutation. Management records also expose aggregate `click_count` and UTC `last_accessed_at`. Only successful active, unexpired public resolutions are counted; no visit events, IP addresses, user agents, referrers, or other visitor metadata are stored. Aggregate tracking is disabled unless both the management token and the internal `-stats-proxy-url` are configured; the production LiteFS command supplies its loopback proxy origin.

### Admin web UI

The optional dependency-free admin console is served at `/admin`. It uses the existing JSON management API and includes create, search, copy, edit, expiration, disable/enable, and confirmed delete flows. Active, disabled, and expired states are shown separately, with quiet aggregate click and last-accessed details. Configure it with the management token plus two additional values:

```sh
export GOTTEM_MANAGEMENT_TOKEN='...'
export GOTTEM_SESSION_SECRET='at-least-32-random-bytes-kept-secret'
export GOTTEM_ADMIN_ORIGIN='https://gottem.link'
go run . -addr :8080 -dsn ./gottem.db
```

`GOTTEM_ADMIN_ORIGIN` is the exact browser origin, with no trailing slash or explicit default port (`:443` for HTTPS or `:80` for HTTP). HTTPS is required outside local development. For local HTTP testing only, loopback origins such as `http://127.0.0.1:8080` and `http://localhost:8080` are accepted:

```sh
export GOTTEM_ADMIN_ORIGIN='http://127.0.0.1:8080'
```

Login verifies `GOTTEM_MANAGEMENT_TOKEN` once and stores a signed 8-hour `HttpOnly`, `SameSite=Strict` cookie—not the token. Cookies are `Secure` on HTTPS; local loopback HTTP is the only exception. Session signatures are bound to the current management-token fingerprint, so rotating that token invalidates existing sessions. Cookie-authenticated writes and login/logout require an exact `Origin` match. Existing bearer-authenticated API and CLI requests remain compatible and do not require an `Origin` header. The UI and session routes remain unavailable unless the complete configuration is valid.

The frontend is embedded HTML, CSS, and vanilla JavaScript with no runtime CDN or Node dependency. HTMX remains a possible future enhancement only if real interaction needs justify it.

## Management CLI

Build the CLI from a checkout and provide the same management token used by the server:

```sh
go install ./cmd/gottem
export GOTTEM_MANAGEMENT_TOKEN='...'
```

The CLI targets `https://gottem.link` by default. Set `GOTTEM_BASE_URL` for another server, or pass `--base-url URL` to override it for one command. Global flags must appear before the command.

```sh
gottem create [--slug SLUG] [--expires-at RFC3339] URL
gottem list
gottem get SLUG
gottem update SLUG URL
gottem disable SLUG
gottem expire SLUG RFC3339
gottem unexpire SLUG
gottem delete [--force] SLUG
gottem export
gottem import [--apply] FILE
```

Pass `--json` before CRUD commands for machine-readable output, including usage statistics. Export writes portable format version 3, preserving lifecycle and aggregate usage fields; import remains compatible with versions 1 and 2, dry-runs by default, and writes only with `--apply`. Delete prompts for confirmation unless `--force` is supplied.

## Checks

`make check` runs formatting, module tidiness, tests, race tests, vet, `govulncheck`, and a build. CI runs the same command for pull requests and `main`.

## Deployment

Merges to `main` deploy `gottem-link` through the `Fly Deploy` GitHub environment. Fly runs two machines in `sjc`; each has a LiteFS volume, and LiteFS proxies public traffic to the Go server. On candidate startup, LiteFS promotes the node, runs `run-app -migrate-only`, and only then starts the server on every node. Redirect GETs remain local; aggregate increments use authenticated internal POSTs through the loopback LiteFS proxy so replicas forward those writes to the primary.

- `make container-test` builds the production image and exercises its LiteFS entrypoint under Docker.
- `/.well-known/healthz` reports process health; `/.well-known/readyz` verifies database readiness.
- [Operations](docs/operations.md) covers topology, backups, restore testing, and rollback.

Do not change production secrets, volumes, LiteFS topology, or the database schema without a backup and an explicit rollout plan.

## Planning

- [Project review and roadmap](docs/project-review-and-roadmap.md)
