-- ============================================================================
-- ROLLBACK GROUP MEMBERSHIP MIGRATION
-- This migration rolls back the group_members table and related functions.
-- ============================================================================

-- Drop the trigger
DROP TRIGGER IF EXISTS prevent_everyone_membership ON group_members;

-- Drop the function
DROP FUNCTION IF EXISTS prevent_everyone_group_membership();

-- Drop the table (cascade will automatically drop indexes)
DROP TABLE IF EXISTS group_members CASCADE;
