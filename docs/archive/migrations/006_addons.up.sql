-- Create administrators table
CREATE TABLE IF NOT EXISTS administrators (
    user_id UUID NOT NULL,
    subject VARCHAR(255) NOT NULL,
    subject_type VARCHAR(20) NOT NULL CHECK (subject_type IN ('user', 'group')),
    granted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    granted_by UUID,
    notes TEXT,
    PRIMARY KEY (user_id, subject, subject_type),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (granted_by) REFERENCES users(id) ON DELETE SET NULL
);

-- Create indexes for administrators
CREATE INDEX idx_administrators_subject ON administrators(subject);
CREATE INDEX idx_administrators_subject_type ON administrators(subject_type);
CREATE INDEX idx_administrators_granted_at ON administrators(granted_at DESC);

-- Create addons table
CREATE TABLE IF NOT EXISTS addons (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    name VARCHAR(255) NOT NULL,
    webhook_id UUID NOT NULL,
    description TEXT,
    icon VARCHAR(60),
    objects TEXT[],
    threat_model_id UUID,
    FOREIGN KEY (webhook_id) REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    FOREIGN KEY (threat_model_id) REFERENCES threat_models(id) ON DELETE CASCADE
);

-- Create indexes for addons
CREATE INDEX idx_addons_webhook ON addons(webhook_id);
CREATE INDEX idx_addons_threat_model ON addons(threat_model_id) WHERE threat_model_id IS NOT NULL;
CREATE INDEX idx_addons_created_at ON addons(created_at DESC);

-- Create addon_invocation_quotas table
CREATE TABLE IF NOT EXISTS addon_invocation_quotas (
    owner_id UUID PRIMARY KEY,
    max_active_invocations INT NOT NULL DEFAULT 1,
    max_invocations_per_hour INT NOT NULL DEFAULT 10,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Create trigger for addon_invocation_quotas modified_at
CREATE TRIGGER update_addon_invocation_quotas_modified_at
    BEFORE UPDATE ON addon_invocation_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_webhook_modified_at();

-- Create index for quota lookups
CREATE INDEX idx_addon_invocation_quotas_owner ON addon_invocation_quotas(owner_id);
