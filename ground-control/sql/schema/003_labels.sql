-- +goose Up

CREATE TABLE labels (
  id SERIAL PRIMARY KEY,
  label_name VARCHAR(255) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE labels;

