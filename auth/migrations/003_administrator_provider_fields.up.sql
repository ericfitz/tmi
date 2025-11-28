-- Migration: Restructure administrators table to use dual foreign key pattern
-- This migration updates the administrators table to follow the same pattern as threat_model_access:
-- - Dual foreign keys (user_internal_uuid OR group_internal_uuid)
-- - XOR constraint to ensure exactly one is populated
-- - Provider field for group lookups (groups are provider-specific)

-- Since we're changing the primary key structure significantly, we need to recreate the table
-- Save any existing data first (though there likely isn't any in development)
CREATE TABLE administrators_backup AS SELECT * FROM administrators;

-- Drop the existing table
DROP TABLE IF EXISTS administrators CASCADE;

-- Create the new administrators table with dual foreign key pattern
CREATE TABLE administrators (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    -- Dual foreign keys: exactly one populated based on subject_type
    user_internal_uuid UUID REFERENCES users(internal_uuid) ON DELETE CASCADE,
    group_internal_uuid UUID REFERENCES groups(internal_uuid) ON DELETE CASCADE,

    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'group')),

    -- Provider field for principal matching (especially important for groups)
    -- For users: redundant with users.provider but kept for query efficiency
    -- For groups: necessary since groups table has provider field
    provider TEXT NOT NULL,

    -- Enforce exactly one subject (XOR constraint)
    CONSTRAINT exactly_one_subject CHECK (
        (subject_type = 'user' AND user_internal_uuid IS NOT NULL AND group_internal_uuid IS NULL) OR
        (subject_type = 'group' AND group_internal_uuid IS NOT NULL AND user_internal_uuid IS NULL)
    ),

    granted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    granted_by_internal_uuid UUID REFERENCES users(internal_uuid) ON DELETE SET NULL,
    notes TEXT,

    -- Ensure uniqueness: one admin entry per user or group
    UNIQUE NULLS NOT DISTINCT (user_internal_uuid, subject_type),
    UNIQUE NULLS NOT DISTINCT (group_internal_uuid, subject_type, provider)
);

-- Create indexes for efficient lookups
CREATE INDEX idx_administrators_user ON administrators(user_internal_uuid) WHERE user_internal_uuid IS NOT NULL;
CREATE INDEX idx_administrators_group ON administrators(group_internal_uuid, provider) WHERE group_internal_uuid IS NOT NULL;
CREATE INDEX idx_administrators_provider ON administrators(provider);
CREATE INDEX idx_administrators_granted_at ON administrators(granted_at DESC);

-- Drop the backup table (no migration of old data - clean slate approach per requirements)
DROP TABLE IF EXISTS administrators_backup;
