-- Rollback: Remove description column from diagrams table

ALTER TABLE diagrams DROP COLUMN IF EXISTS description;
