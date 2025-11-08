-- Drop addon_invocation_quotas table
DROP TRIGGER IF EXISTS update_addon_invocation_quotas_modified_at ON addon_invocation_quotas;
DROP TABLE IF EXISTS addon_invocation_quotas CASCADE;

-- Drop addons table
DROP TABLE IF EXISTS addons CASCADE;

-- Drop administrators table
DROP TABLE IF EXISTS administrators CASCADE;
