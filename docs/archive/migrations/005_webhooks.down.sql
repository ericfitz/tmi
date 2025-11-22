-- Drop triggers
DROP TRIGGER IF EXISTS webhook_subscription_change_notify ON webhook_subscriptions;
DROP TRIGGER IF EXISTS update_webhook_quotas_modified_at ON webhook_quotas;
DROP TRIGGER IF EXISTS update_webhook_subscriptions_modified_at ON webhook_subscriptions;

-- Drop functions
DROP FUNCTION IF EXISTS notify_webhook_subscription_change();
DROP FUNCTION IF EXISTS update_webhook_modified_at();

-- Drop tables (order matters due to foreign keys)
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_url_deny_list;
DROP TABLE IF EXISTS webhook_quotas;
DROP TABLE IF EXISTS webhook_subscriptions;
