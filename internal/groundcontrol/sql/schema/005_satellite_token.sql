-- +goose Up

CREATE TABLE satellite_token (
  id SERIAL PRIMARY KEY,
  satellite_id INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
  token VARCHAR(64) UNIQUE NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE satellite_token;
