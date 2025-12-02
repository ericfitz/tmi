-- Migration rollback: Remove timeout tracking for webhook subscriptions
-- Version: 003

-- Remove timeout_count column from webhook_subscriptions table
ALTER TABLE webhook_subscriptions
DROP COLUMN IF EXISTS timeout_count;
