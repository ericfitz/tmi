-- Migration Rollback: Revert administrators table to original schema
-- This reverses the dual foreign key pattern and restores the simple structure

-- Drop the new table
DROP TABLE IF EXISTS administrators CASCADE;

-- Recreate the original administrators table structure
CREATE TABLE IF NOT EXISTS administrators (
    user_internal_uuid UUID NOT NULL REFERENCES users(internal_uuid) ON DELETE CASCADE,
    subject TEXT NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'group')),
    granted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    granted_by_internal_uuid UUID REFERENCES users(internal_uuid) ON DELETE SET NULL,
    notes TEXT,
    PRIMARY KEY (user_internal_uuid, subject, subject_type)
);

-- Recreate original indexes
CREATE INDEX idx_administrators_subject ON administrators(subject);
CREATE INDEX idx_administrators_subject_type ON administrators(subject_type);
CREATE INDEX idx_administrators_granted_at ON administrators(granted_at DESC);
