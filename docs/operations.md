# Operations

## Topology

`gottem-link` runs on two Fly Machines in `sjc`, each with a 10 GB `litefs` volume. LiteFS uses a Consul lease to elect the primary, exposes HTTP on port 8080, and proxies to the Go server on port 8081. The application reads `/litefs/gottem.db`; LiteFS stores replication data under `/var/lib/litefs`.

Machines may stop when idle. `primary_region` in `fly.toml` must match the machine and volume region or no machine can become the LiteFS primary.

## Health

- `GET /.well-known/healthz` returns 200 when the Go process can serve HTTP.
- `GET /.well-known/readyz` returns 200 only when the `redirects` table can be queried; otherwise it returns 503. Fly uses this to gate deployments.

The multi-segment namespace keeps these operational routes from shadowing existing one-segment redirect slugs.

## Schema migrations

`run-app -migrate-only -dsn PATH` is the only production schema-writing mode. LiteFS runs it only on candidate nodes after mount and synchronization; `lease.promote: true` makes that candidate the writer before the command runs. Normal server startup uses `db.Open`, which does not connect, create tables, or migrate.

Schema version 1 requires case-insensitively unique, non-null slugs; non-null destinations; and creation/update timestamps. Migration from the legacy version-0 table is transactional, preserves IDs and destinations, and fails without changing the original database when legacy rows violate the new constraints. Versions newer than the binary supports are rejected.

Before merging a schema change, export and validate a production backup, run `make container-test`, and confirm that live machine/volume regions still match `primary_region`. After deployment, verify schema version, row count, readiness, and known redirects.

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

4. Store the backup outside the repository. The validator accepts the legacy version-0 schema and the current version-1 schema, while rejecting unknown versions or missing required columns. Remove the remote temporary file after confirming the stored copy.

## Restore

Run `make backup-test` to exercise backup validation and restoration against a disposable SQLite database.

A production restore replaces the live database and requires explicit approval. Validate the backup first, upload it to the current primary, run `litefs import -name gottem.db PATH`, then verify `/.well-known/readyz` and known redirects. Never copy a database directly into `/litefs` or `/var/lib/litefs`.