# Pending schema changes

SQL for Harbor 3 plan items that is written but not applied. goose embeds only `sql/schema/*.sql` (`sql/schema.go`) and sqlc reads only `sql/queries` (`sqlc.yaml`), so nothing here reaches a database or the generated code.

| File | Plan item | Moves to |
|---|---|---|
| `satellite_status_partitions.sql` | 4.5 | `sql/schema/<next free number>_satellite_status_partitions.sql`. From then on `PartitionMaintainer` (`adapters/postgres`) owns status retention and the cleanup job stops calling `DeleteOldSatelliteStatus` |

`internal/groundcontrol/sql/pending_partitions_test.go` applies the file as the next migration and reverts it, so the DDL stays valid. The 3.4 constraints moved to `sql/schema/023_constraints.sql`; the operator notes on duplicate robots and timestamps are in `docs/guides/groundcontrol.md` (Upgrading).
