-- Migration: Add description column to diagrams table
-- This column stores an optional description for diagrams

ALTER TABLE diagrams ADD COLUMN IF NOT EXISTS description TEXT;
