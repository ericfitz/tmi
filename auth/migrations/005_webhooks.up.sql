-- Create webhook_subscriptions table
CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_id UUID NOT NULL,
    threat_model_id UUID NULL,
    name VARCHAR(255) NOT NULL,
    url VARCHAR(2048) NOT NULL,
    events TEXT[] NOT NULL,
    secret VARCHAR(128),
    status VARCHAR(50) NOT NULL DEFAULT 'pending_verification'
        CHECK (status IN ('pending_verification', 'active', 'pending_delete')),
    challenge VARCHAR(64),
    challenges_sent INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_successful_use TIMESTAMPTZ,
    publication_failures INT NOT NULL DEFAULT 0,
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (threat_model_id) REFERENCES threat_models(id) ON DELETE CASCADE
);

-- Create indexes for webhook_subscriptions
CREATE INDEX idx_webhook_subscriptions_owner ON webhook_subscriptions(owner_id);
CREATE INDEX idx_webhook_subscriptions_threat_model ON webhook_subscriptions(threat_model_id) WHERE threat_model_id IS NOT NULL;
CREATE INDEX idx_webhook_subscriptions_status ON webhook_subscriptions(status);
CREATE INDEX idx_webhook_subscriptions_pending_verification ON webhook_subscriptions(status, challenges_sent, created_at) WHERE status = 'pending_verification';
CREATE INDEX idx_webhook_subscriptions_active ON webhook_subscriptions(status) WHERE status = 'active';

-- Create webhook_deliveries table (using UUIDv7 for time-ordered IDs)
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY,
    subscription_id UUID NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at TIMESTAMPTZ,
    FOREIGN KEY (subscription_id) REFERENCES webhook_subscriptions(id) ON DELETE CASCADE
);

-- Create indexes for webhook_deliveries
CREATE INDEX idx_webhook_deliveries_subscription ON webhook_deliveries(subscription_id);
CREATE INDEX idx_webhook_deliveries_status_retry ON webhook_deliveries(status, next_retry_at) WHERE status = 'pending';
CREATE INDEX idx_webhook_deliveries_created ON webhook_deliveries(created_at);

-- Create webhook_quotas table for per-owner rate limits
CREATE TABLE IF NOT EXISTS webhook_quotas (
    owner_id UUID PRIMARY KEY,
    max_subscriptions INT NOT NULL DEFAULT 10,
    max_events_per_minute INT NOT NULL DEFAULT 12,
    max_subscription_requests_per_minute INT NOT NULL DEFAULT 10,
    max_subscription_requests_per_day INT NOT NULL DEFAULT 20,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Create webhook_url_deny_list table for SSRF prevention
CREATE TABLE IF NOT EXISTS webhook_url_deny_list (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    pattern VARCHAR(512) NOT NULL,
    pattern_type VARCHAR(20) NOT NULL CHECK (pattern_type IN ('glob', 'regex')),
    description VARCHAR(1024),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create index for deny list lookups
CREATE INDEX idx_webhook_url_deny_list_pattern_type ON webhook_url_deny_list(pattern_type);

-- Seed default deny list patterns for SSRF prevention
INSERT INTO webhook_url_deny_list (pattern, pattern_type, description) VALUES
    -- Localhost variants
    ('localhost', 'glob', 'Block localhost'),
    ('127.*', 'glob', 'Block loopback addresses (127.0.0.0/8)'),
    ('::1', 'glob', 'Block IPv6 loopback'),

    -- Private IPv4 ranges (RFC 1918)
    ('10.*', 'glob', 'Block private network 10.0.0.0/8'),
    ('172.16.*', 'glob', 'Block private network 172.16.0.0/12 (first subnet)'),
    ('172.17.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.18.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.19.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.20.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.21.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.22.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.23.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.24.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.25.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.26.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.27.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.28.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.29.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.30.*', 'glob', 'Block private network 172.16.0.0/12'),
    ('172.31.*', 'glob', 'Block private network 172.16.0.0/12 (last subnet)'),
    ('192.168.*', 'glob', 'Block private network 192.168.0.0/16'),

    -- Link-local addresses
    ('169.254.*', 'glob', 'Block link-local addresses (169.254.0.0/16)'),
    ('fe80:*', 'glob', 'Block IPv6 link-local addresses (fe80::/10)'),

    -- Private IPv6 ranges
    ('fc00:*', 'glob', 'Block IPv6 unique local addresses (fc00::/7)'),
    ('fd00:*', 'glob', 'Block IPv6 unique local addresses (fd00::/8)'),

    -- Cloud metadata endpoints
    ('169.254.169.254', 'glob', 'Block AWS/Azure/GCP metadata service'),
    ('fd00:ec2::254', 'glob', 'Block AWS IMDSv2 IPv6 metadata service'),
    ('metadata.google.internal', 'glob', 'Block GCP metadata service'),
    ('169.254.169.123', 'glob', 'Block DigitalOcean metadata service'),

    -- Kubernetes internal services
    ('kubernetes.default.svc', 'glob', 'Block Kubernetes internal service'),
    ('10.96.0.*', 'glob', 'Block common Kubernetes service CIDR'),

    -- Docker internal network
    ('172.17.0.1', 'glob', 'Block Docker default bridge gateway'),

    -- Broadcast addresses
    ('255.255.255.255', 'glob', 'Block broadcast address'),
    ('0.0.0.0', 'glob', 'Block null address')
ON CONFLICT DO NOTHING;

-- Create function to update modified_at timestamp
CREATE OR REPLACE FUNCTION update_webhook_modified_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.modified_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create triggers for modified_at updates
CREATE TRIGGER update_webhook_subscriptions_modified_at
    BEFORE UPDATE ON webhook_subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION update_webhook_modified_at();

CREATE TRIGGER update_webhook_quotas_modified_at
    BEFORE UPDATE ON webhook_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_webhook_modified_at();

-- Create notification trigger for subscription changes (for worker wake-up)
CREATE OR REPLACE FUNCTION notify_webhook_subscription_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('webhook_subscription_change', json_build_object(
        'operation', TG_OP,
        'subscription_id', COALESCE(NEW.id, OLD.id),
        'status', COALESCE(NEW.status, OLD.status)
    )::text);
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER webhook_subscription_change_notify
    AFTER INSERT OR UPDATE OR DELETE ON webhook_subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION notify_webhook_subscription_change();
