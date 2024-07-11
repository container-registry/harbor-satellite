-- +goose Up

CREATE TABLE groups (
  id SERIAL PRIMARY KEY,
  group_name VARCHAR(255) NOT NULL,
  username VARCHAR(255) NOT NULL,
  password VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO groups (group_name, username, password) VALUES ('admin_group', 'admin_user', 'admin_pass');
INSERT INTO groups (group_name, username, password) VALUES ('dev_group', 'dev_user', 'dev_pass');
INSERT INTO groups (group_name, username, password) VALUES ('test_group', 'test_user', 'test_pass');

-- +goose Down
DROP TABLE groups;

