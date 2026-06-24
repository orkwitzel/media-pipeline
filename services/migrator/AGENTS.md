# AGENTS.md — migrator

## Ownership

`migrator` owns the `jobs` schema. It is the single source of truth for the Postgres schema. See `docs/contracts.md` for the canonical column definitions — the migration SQL must match that document exactly.

## Adding Migrations

- Add new files as `migrations/NNNN_name.sql` (e.g. `0002_add_index.sql`), incrementing the numeric prefix.
- Never edit or delete a migration file that has already been applied in any environment. The `schema_migrations` ledger tracks applied files by filename; editing an applied file will cause it to be silently skipped while the schema diverges.
- All SQL must use `IF NOT EXISTS` / `IF EXISTS` guards so re-running from scratch is safe.

## Idempotency Contract

`migrator` must be safe to run multiple times against the same database. Applied migrations are skipped via the ledger. New migrations are applied in order.

## Exit Codes

- `0` — all migrations applied (or already up-to-date).
- Non-zero — connection failure, SQL error, or any unexpected problem.

This contract is required for Kubernetes initContainer / Job ordering: downstream services only start after `migrator` exits 0.
