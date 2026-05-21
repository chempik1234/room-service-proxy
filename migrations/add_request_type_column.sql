-- Migration: Add request_type column to request_logs table
-- Date: 2025-01-21
-- Description: Adds the request_type column to track unary vs stream requests

-- Add the request_type column if it doesn't exist
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS request_type TEXT;

-- Create index for request_type to improve query performance
CREATE INDEX IF NOT EXISTS idx_request_logs_request_type ON request_logs(request_type);

-- Update existing records to have a default value
UPDATE request_logs SET request_type = 'unary' WHERE request_type IS NULL;