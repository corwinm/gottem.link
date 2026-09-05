# gottem.link Project Review and Roadmap

> **For Hermes:** Turn the next uncompleted roadmap item into a focused implementation plan, then execute it with tests and review before starting another item.

**Goal:** Recover the existing service, make the repository safe and predictable for coding agents, and grow it into a useful personal URL shortener.

**Architecture direction:** Keep the application as a small Go service backed by SQLite. Establish explicit configuration, a long-lived database dependency, tested HTTP handlers, and a private management API before adding UI or analytics. Reassess whether LiteFS is useful for a single-region, single-owner deployment rather than expanding the infrastructure by default.

**Tech stack:** Go 1.27, `net/http`, SQLite, Fly.io, GitHub Actions.

---

## Current status

Updated against `main` at `87a5242` on 2026-09-05.

- **Complete:** Milestones 0–2 are deployed. The service has a tested redirect path, authenticated management API and CLI, import/export, encrypted backups, a private admin UI, a public homepage, lifecycle controls, and privacy-conscious aggregate usage statistics.
- **Complete:** Go 1.27 is aligned across local, CI, Docker, and Fly builds; formatting, tests, race tests, vet, vulnerability scanning, builds, backup restoration, and production-image smoke tests are enforced.
- **Complete:** schema version 4 is applied transactionally by the candidate-only LiteFS migration phase. Existing data is preserved, serving remains non-migrating, and unknown newer schemas fail closed.
- **Complete:** production runs in `sjc` on encrypted Fly volumes with LiteFS, health/readiness checks, graceful shutdown, restore verification, and a documented deployment path.
- **Complete:** aggregate click count and last-accessed time are available without retaining IP addresses, user agents, referrers, or raw visit events.
- **Complete:** Milestone 3.1 adds stateless authenticated QR preview/download for existing links when the admin UI is fully configured.
- **Next:** Remaining Milestone 3 enhancements are unprioritized options.

The service is live at `https://gottem.link`; routine link management no longer requires direct database access.

## Verified current state

The Milestone 2.4 deployment at `87a5242` passed GitHub Quality, Container, and Deploy jobs. Production health and readiness returned 200, schema version 4 passed `PRAGMA quick_check`, and a disposable production link verified aggregate click tracking before being removed.

## Original findings

The findings below describe the repository at the original review commit. Use the current-status section and roadmap status labels for present state.

### P0 — Redirects do not work correctly

1. **The configured database path is ignored.** `main.go:13` creates the `-dsn` flag but discards its pointer. `handlers/redirect.go:19` reads `flag.Arg(1)`, which is the second positional argument rather than the `-dsn` value. The production command in `litefs.yml:35` supplies `-dsn /litefs/gottem.db`, so handlers open an empty temporary SQLite database instead of that file.

   This was reproduced locally: a known slug in a seeded database returns 404 when the server is started with `-dsn <database>`.

2. **A found link returns 200 rather than a redirect.** `handlers/redirect.go:34` writes a response body before `http.Redirect` is called on line 35. The first write commits HTTP 200; the later 302 is ignored.

   This was reproduced locally with a seeded database. The response was `HTTP/1.1 200 OK` with an HTML link, and the server logged `superfluous response.WriteHeader`.

### P1 — The service cannot manage links

- `DbWrapper` has insert and delete methods, but no route or CLI exposes them.
- There is no authentication boundary for future write operations.
- There is no update, list, search, disable, or import/export workflow.
- There is no supported initial-data or migration workflow.

### P1 — Persistence and error handling are fragile

- Every redirect request opens a database, runs `CREATE TABLE IF NOT EXISTS`, and closes it.
- The schema does not require a unique slug and allows null or empty values.
- Schema creation is assembled with string concatenation. Its current inputs are internal constants, but this is unnecessary risk and not a migration strategy.
- All query errors become 404, including operational database failures that should be 500.
- The application does not verify the database connection at startup.
- Database close errors are discarded.

### P1 — The production-pinned Go toolchain has reachable security vulnerabilities

- The Fly build explicitly sets Go 1.24.1 through `fly.toml`; `go.mod` also declares 1.24.1. CI uses the broader `^1.24.1` range, while a direct Docker build defaults `GO_VERSION` to `1`, so those two surfaces may resolve a newer toolchain and do not reliably reproduce production.
- Scanning the production-pinned Go 1.24.1 build reports 25 reachable standard-library vulnerabilities, including request smuggling, memory-exhaustion, TLS, URL-parsing, and `database/sql` findings.
- The scan is clean when the source is analyzed under the automatically selected Go 1.26.8 toolchain. Upgrade the production toolchain, align every build surface on the same supported version, rerun all tests and the container build, and add patch-version update automation.

### P1 — There is no regression safety

- There are no unit, handler, database, integration, or migration tests.
- CI runs only `go build`; it does not run tests, race tests, vet, formatting checks, module-tidiness checks, or vulnerability checks.
- The workflow grants `actions: write` even though its jobs do not need it.
- Deployment occurs after the build job, but the deployment job is not attached to the existing GitHub environment.
- Workflow actions use mutable tags rather than pinned commit SHAs.

### P2 — HTTP and operational behavior are prototype-level

- The server uses `http.ListenAndServe` directly with no read, header, write, or idle timeouts and no graceful shutdown.
- The root page is still `Hello, World!`.
- There are no health/readiness endpoints or build/version metadata.
- Logs are unstructured and one database error is printed with `fmt.Println`.
- The startup URL is malformed for explicit host addresses (`http://localhost127.0.0.1:...`).
- Methods are not constrained; behavior is based only on paths.
- Slugs are silently lowercased, but that contract is undocumented and untested.

### P2 — Reassess deployment complexity only when changing persistence

The existing Fly.io deployment should be preserved while application defects are fixed. The checked-in configuration designates `sea` as the primary region, defines a LiteFS volume mount, uses a shared CPU, enables scale-to-zero, and configures LiteFS with a Consul lease. It does not prove the live Machine or volume inventory. When persistence is deliberately changed, inspect that inventory and decide whether this personal service still needs replicas or write failover. Plain SQLite on one Fly volume could have fewer moving parts, but replacing working LiteFS is not an immediate roadmap requirement.

## Agent-friendly development plan

Use one portable source of repository instructions rather than separate Hermes-, pi-, and Codex-specific documents.

1. **Add a concise root `AGENTS.md`.** Document architecture, exact commands, configuration, invariants, security boundaries, and the definition of done. Hermes and Codex can both consume it; verify pi's current discovery behavior before adding any adapter file.
2. **Make the local loop deterministic.** Standardize commands for format check, test, race test, vet, vulnerability scan, build, and local run. A small `Makefile` is acceptable if each target is a transparent Go command; avoid a custom task framework.
3. **Add tests before structural refactors.** Handler tests should reproduce the current DSN and HTTP-status bugs. Database tests should use temporary directories and real SQLite.
4. **Inject dependencies explicitly.** Parse configuration once in `main`, open and migrate the database once, then construct handlers/router with a repository dependency. Do not let handlers read global flags.
5. **Keep work items vertically sliced.** Each issue should name behavior, acceptance criteria, affected surfaces, and verification commands. Prefer one reviewable behavior per PR.
6. **Make CI match `AGENTS.md`.** Agents should be able to run the exact same required checks locally. Deployment must depend on those checks and run only from `main`.
7. **Document decisions, not agent transcripts.** Record durable architecture choices in short decision notes only when a real tradeoff is settled.
8. **Protect generated and persistent state.** Tests must never touch the production database. Agents must not deploy, rotate secrets, alter Fly volumes, or run destructive migrations without explicit approval.

### Proposed `AGENTS.md` contents

Keep it short and update it as commands become real:

- Purpose and personal-service scope.
- Package map and request flow.
- Supported Go/toolchain version.
- Local database location and configuration contract.
- Required pre-push checks, in their exact order.
- HTTP and slug behavior invariants.
- Authentication and URL-validation rules for management endpoints.
- Migration rules: forward-only, tested, backed up before production application.
- Deployment boundary: PRs validate; `main` deploys; agents need approval for production operations.
- Definition of done: tests added, all checks pass, docs updated when public behavior changes, no unrelated churn.

## Product and engineering roadmap

### Milestone 0 — Recover a trustworthy redirect service

#### 0.0 Upgrade the Go toolchain — Complete

- Move `go.mod`, CI, and the Docker build to the same currently supported Go release.
- Run tests, race tests, vet, build, `go mod tidy -diff`, and `govulncheck` under that toolchain.
- Add automated patch-version update PRs so the deployed standard library does not remain frozen.

**Done when:** all build surfaces use one supported Go version and `govulncheck ./...` reports no reachable vulnerabilities.

#### 0.1 Characterize and fix configuration — Complete except startup validation

- Add failing tests proving that a configured database is used.
- Parse `-addr` and `-dsn` into an application configuration object.
- Open the database once at startup and inject it into the router/handlers.
- Fail startup with a useful error if the database cannot open or migrate.

**Done when:** the production-style command reads a seeded persistent database, restart preserves its links, and tests fail if the DSN is ignored.

#### 0.2 Fix redirect HTTP semantics — Complete

- Do not write a response body before `http.Redirect`.
- Define case sensitivity and slug syntax explicitly.
- Distinguish not-found errors from database failures.
- Add handler tests for found, missing, malformed, and database-error cases.

**Done when:** a found slug returns the chosen 3xx status and `Location`, a missing slug returns 404, and operational failures return 500 without leaking internals.

#### 0.3 Establish schema and migrations — Complete

- Replace request-time table creation with startup migrations.
- Make `slug` unique and non-null; make `url` non-null.
- Add timestamps needed by management features.
- Test fresh-database creation and migration idempotence.

**Done when:** duplicate slugs fail predictably and a fresh database reaches the expected schema through a repeatable command/startup path.

**Current note:** the first rollout in PR #5 was reverted after an unsafe startup interaction. The retry separates migration and serving modes, runs migration only after LiteFS mounts and promotes a candidate, and is gated by a validated production backup plus the production-entrypoint container test.

#### 0.4 Add the repository development contract — Complete

- Add `AGENTS.md` and expand `README.md` with setup, run, test, configuration, and deployment architecture.
- Add transparent local quality commands.
- Update CI to enforce formatting, tests, race tests, vet, build, and a practical vulnerability scan.
- Make production deployment depend on all required checks and associate it with `Fly Deploy`.

**Done when:** a new human or coding agent can clone the repository and run the complete verification loop without guessing.

#### 0.5 Document and regression-test the working deployment — Complete

- Preserve the current Fly.io path while application-level changes are being stabilized.
- Document the current LiteFS/volume topology and decide later whether plain SQLite would be a worthwhile simplification.
- Test the container in an available Docker/compatible environment.
- Add health/readiness endpoints and graceful shutdown.
- Document backup and restore before any production schema change.

**Done when:** the existing deployment is documented, container-tested, health-checked, and has a tested restore procedure. Any persistence simplification should be a separate change with rollback criteria.

### Milestone 1 — Personal URL-shortener MVP

#### 1.1 Authenticated management API — Complete

- Add token-authenticated endpoints to create, list, inspect, update, disable, and delete redirects.
- Keep public redirects unauthenticated; keep all management routes private by default.
- Store the token only in Fly/GitHub secrets, never in the repository.

**Done when:** unauthorized writes are rejected, authorized CRUD is covered by integration tests, and secrets are not logged.

#### 1.2 URL and slug validation — Complete

- Accept only explicitly supported destination schemes (`https`, and `http` only if intentionally allowed).
- Reject credentials, control characters, empty hosts, oversized inputs, and reserved slugs.
- Support a requested custom slug or generate a short collision-resistant slug.
- Return a clear conflict for existing slugs.

**Done when:** validation and collision behavior are deterministic and covered by table-driven tests.

#### 1.3 Practical management client — Complete

- Add a small CLI that uses the same management API for create/list/update/disable/delete.
- Support machine-readable JSON output so Hermes, pi, Codex, and scripts can use it safely.
- Make destructive commands require an explicit slug and confirmation unless a non-interactive force flag is provided.

**Done when:** a link can be created, verified, changed, disabled, and removed without direct database access.

#### 1.4 Import, export, and backup — Complete

- Export redirects in a documented JSON or CSV format.
- Import with dry-run validation and conflict reporting.
- Automate encrypted database backups and document restore verification.

**Done when:** the service can be rebuilt from a backup/export in a disposable environment and redirects still resolve correctly.

### Milestone 2 — Daily usability

#### 2.1 Minimal admin web UI — Complete

- Add a private, mobile-friendly page for creating and managing links.
- Include copy-to-clipboard, search, status, and clear conflict/error messages.
- Build on the management API rather than adding a second business-logic path.
- Keep the initial frontend dependency-free: embed HTML, CSS, and vanilla JavaScript in the Go binary. HTMX is a possible later enhancement if observed interaction patterns justify it, not a planned dependency or rewrite.
- Authenticate browsers with a short-lived signed `Secure`, `HttpOnly`, `SameSite=Strict` cookie while preserving bearer-token authentication for the CLI and automation.

**Done when:** routine link management works comfortably from phone and desktop with keyboard-accessible controls.

**Delivered:** the embedded dependency-free console uses short-lived signed browser sessions, exact-origin protection for cookie-authenticated writes, and the existing JSON API. Bearer authentication remains available for the CLI, automation, and backup export; HTMX remains future-only.

#### 2.2 Minimal public homepage — Complete

- Replace the placeholder root response with a small, branded explanation of gottem.link.
- Include a clear admin sign-in link without exposing link creation or management publicly.
- Keep the page dependency-free and embedded in the Go binary alongside the admin UI.
- Avoid marketing sections, account features, analytics scripts, or a frontend framework.

**Done when:** `https://gottem.link/` is useful to a first-time visitor, links to the private admin console, and remains fast and accessible on phone and desktop.

**Delivered:** an embedded, script-free homepage explains the personal shortener and links to `/admin/`, without a public directory or management controls. Exact root routing preserves redirects; the small local stylesheet and page use a five-minute public cache and restrictive security headers. Verified locally on desktop and phone viewports; production rollout is separate.

#### 2.3 Link lifecycle controls — Complete

- Add optional expiration, disable/enable, and destination replacement.
- Preserve enough audit information to understand when a destination changed.
- Decide whether expired slugs can ever be reused; default to no reuse.

**Done when:** lifecycle behavior is explicit, tested, and visible in the API/CLI/UI.

**Delivered:** optional RFC3339 expiration is available at creation and through dedicated set/clear controls in the API, CLI, and embedded admin. Expired links return 404 while records and case-insensitively reserved slugs remain. A separate destination-change timestamp is unaffected by disable, enable, and expiration mutations. Schema version 3 preserves existing rows, portable export version 2 preserves the new lifecycle fields, and legacy version 1 imports remain accepted.

#### 2.4 Privacy-conscious usage statistics — Complete

- Start with aggregate click count and last-accessed time.
- Avoid storing IP addresses, user-agent strings, or referrers unless there is a specific use case and retention policy.
- Ensure analytics writes do not block redirects.

**Done when:** counts are useful for personal cleanup, documented, and do not collect unnecessary visitor data.

**Delivered:** schema version 4 stores only an aggregate successful redirect count and UTC last-accessed timestamp. A bounded single-writer queue keeps redirect responses independent from analytics storage, drains accepted work on clean shutdown, and deliberately drops events on saturation, storage failure, or process loss. Management API, JSON CLI output, the embedded admin UI, portable export version 3, backward-compatible imports, and SQLite backup validation preserve and expose the aggregates without raw visits or visitor metadata.

### Milestone 3 — Optional enhancements

Only prioritize these after normal link management is reliable.

#### 3.1 QR codes — Complete

- Add an authenticated QR-code image endpoint for an existing short link.
- Add a quiet admin action that previews and downloads the QR code.
- Encode the canonical short URL only; do not add public creation controls, tracking parameters, or stored QR artifacts.
- Keep QR generation stateless and covered by API, security, image-decoding, and browser tests.

**Done when:** an authenticated user can preview and download a scannable QR code from the admin UI, the decoded value exactly matches the short URL, and unauthorized or missing links do not expose an image.

**Delivered:** the fully configured admin registers a browser-session-or-bearer-authenticated `GET`/`HEAD` PNG endpoint. It generates a deterministic high-ECC QR code in memory with a four-module quiet zone and no persistence, encoding only the configured canonical origin plus the stored slug even for disabled or expired records. The dependency-free admin adds an accessible preview/download dialog with visible load failure, session-reset cleanup, keyboard focus restoration, and mobile-safe controls.

#### Later options

- Tags and notes for organization.
- Bulk actions and stale-link review.
- Per-link redirect status choice where it has a real use case.
- Additional custom domains.
- Richer aggregate analytics with an explicit privacy policy.

Avoid accounts, teams, billing, distributed caches, event pipelines, or multi-region writes unless the product's actual use expands beyond a personal service.

## Recommended first issues

1. **Complete:** Upgrade the pinned Go toolchain and verify the vulnerability scan.
2. **Complete:** Add failing handler tests for DSN usage and redirect status.
3. **Complete:** Inject a long-lived database into handlers and fix redirect responses.
4. **Complete:** Add a unique schema migration and database integration tests after backup and production-realistic LiteFS validation.
5. **Complete:** Add `AGENTS.md`, complete local commands, and CI quality gates.
6. **Complete:** Document and regression-test the Fly deployment and backup path.
7. **Complete:** Add token-authenticated create and list endpoints.
8. **Complete:** Add inspect, update, disable, and delete endpoints.
9. **Complete:** Add strict URL and slug validation plus generated slugs.
10. **Complete:** Add the JSON-capable management CLI.
11. **Complete:** Add dry-run import/export and restore verification.
12. **Complete:** Add a minimal private admin UI.
13. **Complete:** Add a minimal public homepage.

The first three issues may be grouped into one recovery PR only if the tests remain focused and the diff stays small. Later issues should generally be separate PRs.
