-- name: CreateTenant :one
INSERT INTO tenants (key)
VALUES (sqlc.arg(key))
RETURNING id, key, created_at, updated_at;

-- name: GetTenantByKey :one
SELECT id, key, created_at, updated_at
FROM tenants
WHERE key = sqlc.arg(key);

-- name: ListTenants :many
SELECT id, key, created_at, updated_at
FROM tenants
ORDER BY id;
