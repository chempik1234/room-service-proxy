-- Migration: Drop unused columns from request_logs table
-- Date: 2025-01-21
-- Description: Drops unused path and response_time columns that are NULL and not used by the application

-- Drop unused columns (safe to drop, they are NULL and not referenced by the code)
ALTER TABLE request_logs DROP COLUMN IF EXISTS path;
ALTER TABLE request_logs DROP COLUMN IF EXISTS response_time;

-- Update the request_stats view to remove response_time reference
CREATE OR REPLACE VIEW request_stats AS
SELECT
    tenant_id,
    COUNT(*) as total_requests,
    AVG(latency_ms) as avg_latency,
    COUNT(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 END) as success_requests,
    COUNT(CASE WHEN status_code >= 400 THEN 1 END) as error_requests,
    DATE(created_at) as request_date
FROM request_logs
GROUP BY tenant_id, DATE(created_at);