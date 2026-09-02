# Operations

## Topology

`gottem-link` runs on two Fly Machines in `sjc`, each with a 10 GB `litefs` volume. LiteFS uses a Consul lease to elect the primary, exposes HTTP on port 8080, and proxies to the Go server on port 8081. The application reads `/litefs/gottem.db`; LiteFS stores replication data under `/var/lib/litefs`.

Machines may stop when idle. `primary_region` in `fly.toml` must match the machine and volume region or no machine can become the LiteFS primary.

## Health

- `GET /.well-known/healthz` returns 200 when the Go process can serve HTTP.
- `GET /.well-known/readyz` returns 200 only when the `redirects` table can be queried; otherwise it returns 503. Fly uses this to gate deployments.

The multi-segment namespace keeps these operational routes from shadowing existing one-segment redirect slugs.

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

4. Store the backup outside the repository. Remove the remote temporary file after confirming the stored copy.

## Restore

Run `make backup-test` to exercise backup validation and restoration against a disposable SQLite database.

A production restore replaces the live database and requires explicit approval. Validate the backup first, upload it to the current primary, run `litefs import -name gottem.db PATH`, then verify `/.well-known/readyz` and known redirects. Never copy a database directly into `/litefs` or `/var/lib/litefs`.