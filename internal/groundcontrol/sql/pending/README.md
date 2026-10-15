# Pending schema changes

SQL for Harbor 3 plan items that is written but not applied. goose embeds only `sql/schema/*.sql` (`sql/schema.go`) and sqlc reads only `sql/queries` (`sqlc.yaml`), so nothing here reaches a database or the generated code.

| File | Plan item | Moves to |
|---|---|---|
| `constraints.sql` | 3.4 | `sql/schema/<next free number>_constraints.sql` |
| `configs_version_queries.sql` | 3.4 | appended to `sql/queries/configs.sql`, then `sqlc generate` |
| `satellite_status_partitions.sql` | 4.5 | `sql/schema/<next free number>_satellite_status_partitions.sql`. From then on `PartitionMaintainer` (`adapters/postgres`) owns status retention and the cleanup job stops calling `DeleteOldSatelliteStatus` |

`internal/groundcontrol/sql/migrations_test.go` and `pending_partitions_test.go` apply each file as the next migration and revert it, so the DDL stays valid. The tests in `internal/groundcontrol/server/constraints_test.go` describe the result of 3.4; the change that moves the files removes their `pending.Plan` markers. `UpdateConfig` then uses `UpdateConfigIfVersion` and answers 409 when it updates no row.

## Duplicate robot accounts

The migration stops with `robot_accounts has several robots for satellites: …` when a satellite has more than one row in `robot_accounts`. Keep the newest row per satellite and delete the others:

```sql
SELECT satellite_id, id, robot_name, robot_id, created_at
FROM robot_accounts
WHERE satellite_id IN (SELECT satellite_id FROM robot_accounts GROUP BY satellite_id HAVING count(*) > 1)
ORDER BY satellite_id, created_at DESC, id DESC;

DELETE FROM robot_accounts r
USING robot_accounts newer
WHERE newer.satellite_id = r.satellite_id
  AND (newer.created_at, newer.id) > (r.created_at, r.id);
```

Delete the Harbor robots of the removed rows (`robot_id`) in Harbor as well, then run the migration again.

## Timestamps

`TIMESTAMP` columns become `TIMESTAMPTZ` with `AT TIME ZONE 'UTC'`, which reads every stored value as UTC wall-clock time. Values written with `NOW()` follow the database's `TimeZone` setting, and values written by Ground Control (for example `robot_expiry`, `expires_at`, `locked_until`) follow the time zone of the GC process. Check both are UTC before migrating, or change the `USING` clauses to the zone that was in effect.
