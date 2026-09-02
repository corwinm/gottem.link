# AGENTS.md

Small Go URL shortener using SQLite and LiteFS.

## Work

- Run `make check` before committing.
- Preserve 302 redirects, 404s for missing slugs, and 500s for database failures.
- Never touch production secrets, volumes, deployments, or schema without explicit approval.
- Keep changes focused and update public documentation when behavior changes.

See `README.md` for setup and architecture.
