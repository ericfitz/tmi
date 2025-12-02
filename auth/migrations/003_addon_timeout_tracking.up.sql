-- Migration: Add timeout tracking for webhook subscriptions
-- Version: 003
-- Description: Adds timeout_count column to track consecutive addon invocation timeouts

-- Add timeout_count column to webhook_subscriptions table
ALTER TABLE webhook_subscriptions
ADD COLUMN IF NOT EXISTS timeout_count INT NOT NULL DEFAULT 0;

-- Add comment for documentation
COMMENT ON COLUMN webhook_subscriptions.timeout_count IS 'Count of consecutive addon invocation timeouts. Reset to 0 on successful completion.';
