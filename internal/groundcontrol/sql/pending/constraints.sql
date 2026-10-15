-- Plan item 3.4. Not applied: neither goose nor sqlc reads sql/pending.
-- See README.md before moving it into sql/schema.

-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE dup TEXT;
BEGIN
  SELECT string_agg(satellite_id::TEXT || ' (' || n || ')', ', ') INTO dup
  FROM (SELECT satellite_id, count(*) AS n FROM robot_accounts GROUP BY satellite_id HAVING count(*) > 1) d;
  IF dup IS NOT NULL THEN
    RAISE EXCEPTION 'robot_accounts has several robots for satellites: %; run the cleanup in sql/pending/README.md', dup;
  END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE robot_accounts ADD CONSTRAINT robot_accounts_satellite_id_key UNIQUE (satellite_id);

ALTER TABLE satellite_configs
  DROP CONSTRAINT satellite_configs_config_id_fkey,
  ADD CONSTRAINT satellite_configs_config_id_fkey FOREIGN KEY (config_id) REFERENCES configs(id) ON DELETE RESTRICT;

-- Matches normalizeHeartbeatInterval (server/helpers.go): @every HHhMMmSSs.
ALTER TABLE satellites ADD CONSTRAINT satellites_heartbeat_interval_check
  CHECK (heartbeat_interval IS NULL OR heartbeat_interval ~ '^@every [0-9]{2,}h[0-5][0-9]m[0-5][0-9]s$');

ALTER TABLE configs ADD COLUMN version INT NOT NULL DEFAULT 1;

-- Assumes stored values are UTC wall-clock time; see README.md.
ALTER TABLE satellites
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC',
  ALTER COLUMN last_seen TYPE TIMESTAMPTZ USING last_seen AT TIME ZONE 'UTC';
ALTER TABLE groups
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE robot_accounts
  ALTER COLUMN robot_expiry TYPE TIMESTAMPTZ USING robot_expiry AT TIME ZONE 'UTC',
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE satellite_token
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC',
  ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING expires_at AT TIME ZONE 'UTC';
ALTER TABLE configs
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE satellite_status
  ALTER COLUMN reported_at TYPE TIMESTAMPTZ USING reported_at AT TIME ZONE 'UTC',
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
ALTER TABLE users
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE sessions
  ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING expires_at AT TIME ZONE 'UTC',
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
ALTER TABLE login_attempts
  ALTER COLUMN locked_until TYPE TIMESTAMPTZ USING locked_until AT TIME ZONE 'UTC',
  ALTER COLUMN last_attempt TYPE TIMESTAMPTZ USING last_attempt AT TIME ZONE 'UTC';
ALTER TABLE artifacts
  ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';

-- +goose Down
ALTER TABLE artifacts
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
ALTER TABLE login_attempts
  ALTER COLUMN locked_until TYPE TIMESTAMP USING locked_until AT TIME ZONE 'UTC',
  ALTER COLUMN last_attempt TYPE TIMESTAMP USING last_attempt AT TIME ZONE 'UTC';
ALTER TABLE sessions
  ALTER COLUMN expires_at TYPE TIMESTAMP USING expires_at AT TIME ZONE 'UTC',
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
ALTER TABLE users
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE satellite_status
  ALTER COLUMN reported_at TYPE TIMESTAMP USING reported_at AT TIME ZONE 'UTC',
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
ALTER TABLE configs
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE satellite_token
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC',
  ALTER COLUMN expires_at TYPE TIMESTAMP USING expires_at AT TIME ZONE 'UTC';
ALTER TABLE robot_accounts
  ALTER COLUMN robot_expiry TYPE TIMESTAMP USING robot_expiry AT TIME ZONE 'UTC',
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE groups
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC';
ALTER TABLE satellites
  ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC',
  ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC',
  ALTER COLUMN last_seen TYPE TIMESTAMP USING last_seen AT TIME ZONE 'UTC';

ALTER TABLE configs DROP COLUMN version;

ALTER TABLE satellites DROP CONSTRAINT satellites_heartbeat_interval_check;

ALTER TABLE satellite_configs
  DROP CONSTRAINT satellite_configs_config_id_fkey,
  ADD CONSTRAINT satellite_configs_config_id_fkey FOREIGN KEY (config_id) REFERENCES configs(id) ON DELETE CASCADE;

ALTER TABLE robot_accounts DROP CONSTRAINT robot_accounts_satellite_id_key;
