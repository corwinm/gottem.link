# Operations

## Topology

`gottem-link` runs on two Fly Machines in `sjc`, each with a 10 GB `litefs` volume. LiteFS uses a Consul lease to elect the primary, exposes HTTP on port 8080, and proxies to the Go server on port 8081. The application reads `/litefs/gottem.db`; LiteFS stores replication data under `/var/lib/litefs`.

Machines may stop when idle. `primary_region` in `fly.toml` must match the machine and volume region or no machine can become the LiteFS primary.

## Health

- `GET /.well-known/healthz` returns 200 when the Go process can serve HTTP.
- `GET /.well-known/readyz` returns 200 only when the database is at the current schema version and its required columns can be queried; otherwise it returns 503. Fly uses this to gate deployments.

The multi-segment namespace keeps these operational routes from shadowing existing one-segment redirect slugs.

## Management API

Set `GOTTEM_MANAGEMENT_TOKEN` to enable redirect management and imports; those requests accept its bearer credential. When the admin UI is fully configured as described in the README, redirect management and imports also accept its signed browser session cookie, with exact-origin enforcement on unsafe methods. `GET /api/v1/exports` remains bearer-only and accepts either the management credential or the distinct read-only `GOTTEM_BACKUP_TOKEN`. Public redirects remain unauthenticated. Tokens are read from the environment, compared in constant time, and never included in responses or logs. If neither token is configured, `/api/` returns 404; with only a backup token, other API routes remain unavailable.

- `POST /api/v1/redirects` with `{"slug":"name","url":"https://example.com","expires_at":"2030-01-02T03:04:05Z"}` creates a redirect; `expires_at` is optional.
- `GET /api/v1/redirects` lists redirects, including disabled entries.
- `GET /api/v1/exports` returns the compact versioned portable export.
- `GET /api/v1/redirects/{slug}` inspects one redirect.
- `PUT /api/v1/redirects/{slug}` with `{"url":"https://example.com/new"}` replaces its destination.
- `POST /api/v1/redirects/{slug}/disable` disables public resolution.
- `POST /api/v1/redirects/{slug}/enable` restores public resolution.
- `PUT /api/v1/redirects/{slug}/expiration` with an RFC3339 `expires_at` sets expiration; `{"expires_at":null}` clears it.
- `DELETE /api/v1/redirects/{slug}` permanently removes it.
- `POST /api/v1/imports?dry_run=true` validates a versioned portable export without writing; omit the query to import it atomically.

Destinations must use `http` or `https` and be no more than 2048 bytes. URLs with credentials, invalid or empty hosts, or unsafe characters are rejected. Custom slugs are canonicalized to lowercase and must contain 1–64 ASCII letters or digits, with only single internal hyphens; `api`, `admin`, and the `.well-known` namespace are reserved. Omitting `slug` or setting it to `null` generates a seven-character slug, while an explicitly empty slug is invalid.

Responses are JSON except successful deletion, which returns 204. Validation failures return 400 JSON with a field-specific `field` value; slug conflicts return 409; missing redirects return 404. Keep the production token only in Fly secrets and a secure local credential store.

## Schema migrations

`run-app -migrate-only -dsn PATH` is the only production schema-writing mode. LiteFS runs it only on candidate nodes after mount and synchronization; `lease.promote: true` makes that candidate the writer before the command runs. Normal server startup uses `db.Open`, which does not connect, create tables, or migrate.

Schema version 1 introduced case-insensitively unique, non-null slugs; non-null destinations; and creation/update timestamps. Version 2 adds the nullable disable timestamp used by management operations. Version 3 adds nullable expiration and the non-null destination-change timestamp, backfilled from each row's existing update timestamp. Migration from the legacy version-0 table is transactional, preserves IDs and destinations, and fails without changing the original database when legacy rows violate the new constraints. Versions newer than the binary supports are rejected.

Before merging a schema change, export and validate a production backup, run `make container-test`, and confirm that live machine/volume regions still match `primary_region`. After deployment, verify schema version, row count, readiness, and known redirects.

## Portable export and import

`gottem export` writes a versioned logical backup that can rebuild redirect behavior without database access:

```json
{"version":1,"redirects":[{"slug":"name","url":"https://example.com","disabled":false}]}
```

The format intentionally excludes database IDs and timestamps. Treat exports as sensitive because they contain private destination URLs, and store them encrypted outside the repository.

Validate the entire file and report slug conflicts without writing before applying an import:

```sh
gottem export > redirects-v1.json
gottem import redirects-v1.json
gottem import --apply redirects-v1.json
```

Use `-` instead of a filename to read from standard input. Import is transactional and rejects unsupported versions, unknown or duplicate JSON fields, missing required fields, empty slugs or destinations, duplicate slugs under SQLite's ASCII-only `NOCASE` rules, and existing slug conflicts. As a restore format, it otherwise preserves valid UTF-8 legacy slug and destination values exactly, along with whether each redirect is active or disabled. Export fails explicitly rather than corrupting a legacy row that contains invalid UTF-8.

### Scheduled encrypted logical backups

The `Encrypted Backup` GitHub Actions workflow is scheduled daily at 09:17 UTC and can also be started manually from `main`. Dispatches from other refs are skipped before secrets reach a job. It authenticates only to the export endpoint with the read-only `GOTTEM_BACKUP_TOKEN`, validates the response through the CLI's strict export decoder, encrypts it with `age`, removes the plaintext, and uploads the ciphertext as a GitHub artifact retained for 30 days.

- `GOTTEM_BACKUP_TOKEN` is a secret in the GitHub `Backups` environment and a Fly application secret. The environment permits deployments only from `main`, so branch workflows cannot receive the token. It must be distinct from `GOTTEM_MANAGEMENT_TOKEN`; the server fails startup if they match. It does not authorize management writes or imports.
- `GOTTEM_BACKUP_AGE_RECIPIENT` is a non-secret GitHub Actions variable containing the public `age` recipient.
- The matching private identity stays outside GitHub Actions. Store it in a password manager or offline backup; losing it makes every artifact unrecoverable.
- GitHub's normal workflow-failure notifications are the initial alerting path. GitHub can delay or drop scheduled runs, so an external daily freshness check must alert when no successful backup artifact has been created for 36 hours.
- Before enabling the schedule, derive the recipient from the stored private identity with `age-keygen -y`, confirm it exactly matches `GOTTEM_BACKUP_AGE_RECIPIENT`, then complete one download/decrypt/import drill. Repeat the drill at least quarterly: import into an empty disposable service with `--apply` and verify representative active and disabled redirects. Production restore remains approval-gated.
- Rollout order is: stage the distinct Fly and GitHub backup token plus the public recipient, deploy the backup-token endpoint, manually run the workflow from `main`, and complete the initial restore drill. A backup attempted before the endpoint deploys is expected to fail closed.

## Deploy and rollback

Pull requests run `make check` and the production-image smoke test. A merge to `main` deploys only after both pass. Verify the Actions run, `fly status -a gottem-link`, `/.well-known/healthz`, and `/.well-known/readyz` after deployment.

Rollback application changes with a reviewed revert PR. Do not alter or destroy volumes as an application rollback.

## Backup

Fly takes daily volume snapshots and currently retains them for five days. Before a schema change, also export a database-level backup:

1. Run `fly status -a gottem-link` and identify the started primary Machine.
2. Export a consistent copy on that Machine:

   ```sh
   fly ssh console -a gottem-link --machine MACHINE_ID \
     -C 'rm -f /tmp/gottem-backup.db && litefs export -name gottem.db /tmp/gottem-backup.db'
   ```

3. Download and validate it:

   ```sh
   fly ssh sftp get /tmp/gottem-backup.db ./gottem-backup.db \
     -a gottem-link --machine MACHINE_ID
   scripts/check-sqlite-backup ./gottem-backup.db
   ```

4. Store the backup outside the repository. The validator accepts schema versions 0, 1, and 2 while rejecting incompatible rows, unknown versions, or missing required constraints. Remove the remote temporary file after confirming the stored copy.

## Restore

Run `make backup-test` to exercise backup validation and restoration against a disposable SQLite database.

The management CLI integration test separately exports active and disabled redirects from one disposable service, dry-runs and applies them to another, and verifies equivalent redirect behavior.

A production restore replaces the live database and requires explicit approval. Validate the backup first, upload it to the current primary, run `litefs import -name gottem.db PATH`, then verify `/.well-known/readyz` and known redirects. Never copy a database directly into `/litefs` or `/var/lib/litefs`.