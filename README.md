# gottem.link

A small personal URL shortener written in Go with SQLite.

## Development

Requires Go 1.27, a C compiler for `go-sqlite3`, and `make`.

```sh
make tools
make check
make run
```

The local server listens on `:8080` and stores data in `./gottem.db`. Override either value directly:

```sh
go run . -addr :3000 -dsn /path/to/gottem.db
```

## Checks

`make check` runs formatting, module tidiness, tests, race tests, vet, `govulncheck`, and a build. CI runs the same command for pull requests and `main`.

## Deployment

Merges to `main` deploy `gottem-link` through the `Fly Deploy` GitHub environment. Fly runs two machines in `sjc`; each has a LiteFS volume, and LiteFS proxies public traffic to the Go server.

- `make container-test` builds and exercises the production image.
- `/healthz` reports process health; `/readyz` verifies database readiness.
- [Operations](docs/operations.md) covers topology, backups, restore testing, and rollback.

Do not change production secrets, volumes, LiteFS topology, or the database schema without a backup and an explicit rollout plan.

## Planning

- [Project review and roadmap](docs/project-review-and-roadmap.md)
