-- ============================================================================
-- GROUP MEMBERSHIP MIGRATION
-- This migration creates the group_members table for managing user-group
-- relationships. This enables explicit group membership for users.
-- ============================================================================

-- Create group_members table
CREATE TABLE IF NOT EXISTS group_members (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    group_internal_uuid UUID NOT NULL REFERENCES groups(internal_uuid) ON DELETE CASCADE,
    user_internal_uuid UUID NOT NULL REFERENCES users(internal_uuid) ON DELETE CASCADE,
    added_by_internal_uuid UUID REFERENCES users(internal_uuid) ON DELETE SET NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    notes TEXT,

    -- Ensure one membership record per user-group pair
    UNIQUE (group_internal_uuid, user_internal_uuid)
);

-- ============================================================================
-- INDEXES
-- ============================================================================

-- Index for finding all groups a user belongs to
CREATE INDEX idx_group_members_user ON group_members(user_internal_uuid);

-- Index for finding all members of a group
CREATE INDEX idx_group_members_group ON group_members(group_internal_uuid);

-- Index for audit queries (who added whom)
CREATE INDEX idx_group_members_added_by ON group_members(added_by_internal_uuid) WHERE added_by_internal_uuid IS NOT NULL;

-- Index for time-based queries
CREATE INDEX idx_group_members_added_at ON group_members(added_at DESC);

-- Composite index for performance
CREATE INDEX idx_group_members_group_added_at ON group_members(group_internal_uuid, added_at DESC);

-- ============================================================================
-- CONSTRAINTS
-- ============================================================================

-- Prevent adding users to the special "everyone" pseudo-group
-- (everyone is implicitly a member of this group by authentication)
CREATE OR REPLACE FUNCTION prevent_everyone_group_membership()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.group_internal_uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN
        RAISE EXCEPTION 'Cannot add members to the "everyone" pseudo-group (system reserved)';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER prevent_everyone_membership
BEFORE INSERT OR UPDATE ON group_members
FOR EACH ROW
EXECUTE FUNCTION prevent_everyone_group_membership();
