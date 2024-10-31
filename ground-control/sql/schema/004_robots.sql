-- +goose Up

CREATE TABLE robots (
  id SERIAL PRIMARY KEY,
  robot_name VARCHAR(255) UNIQUE NOT NULL,
  secret VARCHAR(255) NOT NULL,
  satellite_id INT UNIQUE
  REFERENCES satellites(id) ON DELETE SET NULL,
  created_at TIMESTAMP DEFAULT NOW() NOT NULL,
  updated_at TIMESTAMP DEFAULT NOW() NOT NULL
);

-- +goose Down
DROP TABLE robots;

