-- Clean up orphan project row inserted by 00020 when accounts table was empty.
-- The seed project references account_id = '00000000-...-0002' which may not
-- exist yet (created later by SeedDesktopUser). Delete if no matching account.

-- +goose Up
DELETE FROM projects
WHERE account_id NOT IN (SELECT id FROM accounts);

-- +goose Down
SELECT 1;
