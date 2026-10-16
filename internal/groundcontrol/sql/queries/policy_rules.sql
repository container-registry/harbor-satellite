-- name: ListPolicyRules :many
SELECT id, subject, scope, resource_type, action, effect
FROM policy_rules
ORDER BY id;

-- name: InsertPolicyRule :one
INSERT INTO policy_rules (subject, scope, resource_type, action, effect, created_by)
VALUES (sqlc.arg(subject), sqlc.arg(scope), sqlc.arg(resource_type), sqlc.arg(action), sqlc.arg(effect), sqlc.narg(created_by))
RETURNING id, subject, scope, resource_type, action, effect;

-- name: DeletePolicyRule :execrows
DELETE FROM policy_rules WHERE id = $1;

-- name: ListRoleBindings :many
SELECT id, principal, role
FROM policy_role_bindings
ORDER BY id;

-- name: InsertRoleBinding :one
INSERT INTO policy_role_bindings (principal, role, created_by)
VALUES (sqlc.arg(principal), sqlc.arg(role), sqlc.narg(created_by))
RETURNING id, principal, role;

-- name: DeleteRoleBinding :execrows
DELETE FROM policy_role_bindings WHERE id = $1;

-- name: GetPolicyVersion :one
SELECT version FROM policy_version;

-- name: BumpPolicyVersion :one
-- Runs in the transaction of the rule or binding write, so the row lock
-- orders concurrent writes and a reader that sees the new version also
-- sees the write.
UPDATE policy_version SET version = version + 1, updated_at = NOW()
RETURNING version;
