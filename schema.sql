-- RoomService Proxy Database Schema
-- Complete schema with user authentication and multi-tenancy support

-- ============================================
-- USERS & AUTHENTICATION
-- ============================================

-- Create users table for authentication
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user', -- admin, user
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Create indexes for users
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

-- Create auth_tokens table for session management
CREATE TABLE IF NOT EXISTS auth_tokens (
    token TEXT PRIMARY KEY,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create index for auth_tokens
CREATE INDEX IF NOT EXISTS idx_auth_tokens_user_id ON auth_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_expires_at ON auth_tokens(expires_at);

-- Create trigger for auto-updating users updated_at
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================
-- TENANTS
-- ============================================

-- Create tenants table with multi-tenancy support
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE, -- Track tenant ownership (NULL for admin-created tenants)
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    api_key TEXT UNIQUE NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active', -- active, suspended, deleted
    plan TEXT NOT NULL DEFAULT 'free',    -- free, pro, enterprise
    max_rooms INTEGER DEFAULT 50,
    max_rps INTEGER DEFAULT 100,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Create indexes for tenants
CREATE INDEX IF NOT EXISTS idx_tenants_api_key ON tenants(api_key);
CREATE INDEX IF NOT EXISTS idx_tenants_user_id ON tenants(user_id);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenants_plan ON tenants(plan);
CREATE INDEX IF NOT EXISTS idx_tenants_created_at ON tenants(created_at);

-- Create trigger to auto-update updated_at
DROP TRIGGER IF EXISTS update_tenants_updated_at ON tenants;
CREATE TRIGGER update_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================
-- MONITORING & LOGGING
-- ============================================

-- Create request_logs table for monitoring
CREATE TABLE IF NOT EXISTS request_logs (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    method TEXT,
    path TEXT,
    status_code INTEGER,
    latency_ms INTEGER,
    response_time INTEGER, -- Add this field for compatibility
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create index for request logs
CREATE INDEX IF NOT EXISTS idx_request_logs_tenant_id ON request_logs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_request_logs_status_code ON request_logs(status_code);

-- Create usage_stats table for tracking tenant usage
CREATE TABLE IF NOT EXISTS usage_stats (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    stat_date DATE NOT NULL,
    request_count INTEGER DEFAULT 0,
    room_count INTEGER DEFAULT 0,
    unique (tenant_id, stat_date)
);

-- Create index for usage stats
CREATE INDEX IF NOT EXISTS idx_usage_stats_tenant_id ON usage_stats(tenant_id);
CREATE INDEX IF NOT EXISTS idx_usage_stats_stat_date ON usage_stats(stat_date);

-- ============================================
-- SECURITY & PASSWORDS
-- ============================================

-- Create tenant_passwords table for secure password storage
CREATE TABLE IF NOT EXISTS tenant_passwords (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    service_type TEXT NOT NULL, -- mongodb, redis, etc.
    password TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    unique (tenant_id, service_type)
);

-- Create index for tenant_passwords
CREATE INDEX IF NOT EXISTS idx_tenant_passwords_tenant_id ON tenant_passwords(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tenant_passwords_service_type ON tenant_passwords(service_type);

-- Create trigger for auto-updating updated_at
DROP TRIGGER IF EXISTS update_tenant_passwords_updated_at ON tenant_passwords;
CREATE TRIGGER update_tenant_passwords_updated_at
    BEFORE UPDATE ON tenant_passwords
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================
-- UTILITY FUNCTIONS
-- ============================================

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- ============================================
-- SAMPLE DATA (Optional - Commented out)
-- ============================================

-- Insert sample admin user (password: admin123)
-- INSERT INTO users (id, email, password_hash, name, role)
-- VALUES (
--     'usr_admin_001',
--     'admin@roomservice.dev',
--     '$2a$10$YourHashedPasswordHere',
--     'Admin User',
--     'admin'
-- ) ON CONFLICT (email) DO NOTHING;

-- Insert sample tenant
-- INSERT INTO tenants (name, email, api_key, host, port, status, plan)
-- VALUES ('Test Tenant', 'test@example.com', 'rs_live_test_key', 'localhost', 50051, 'active', 'free')
-- ON CONFLICT (api_key) DO NOTHING;

-- ============================================
-- MIGRATION NOTES
-- ============================================

-- For existing databases, run these migrations:
--
-- 1. Add user_id column to existing tenants table:
--    ALTER TABLE tenants ADD COLUMN IF NOT EXISTS user_id TEXT REFERENCES users(id) ON DELETE CASCADE;
--    CREATE INDEX IF NOT EXISTS idx_tenants_user_id ON tenants(user_id);
--
-- 2. Add response_time column to request_logs if using old schema:
--    ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS response_time INTEGER;
--
-- 3. Create users table if it doesn't exist (see above)
-- 4. Create auth_tokens table if it doesn't exist (see above)

-- ============================================
-- VIEWS FOR COMMON QUERIES
-- ============================================

-- Create view for active tenants with user info
CREATE OR REPLACE VIEW active_tenants_with_users AS
SELECT
    t.id,
    t.user_id,
    t.name,
    t.email,
    t.api_key,
    t.host,
    t.port,
    t.status,
    t.plan,
    t.max_rooms,
    t.max_rps,
    t.created_at,
    t.updated_at,
    u.name as user_name,
    u.email as user_email,
    u.role as user_role
FROM tenants t
LEFT JOIN users u ON t.user_id = u.id
WHERE t.status = 'active';

-- Create view for request statistics
CREATE OR REPLACE VIEW request_stats AS
SELECT
    tenant_id,
    COUNT(*) as total_requests,
    AVG(latency_ms) as avg_latency,
    AVG(response_time) as avg_response_time,
    COUNT(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 END) as success_requests,
    COUNT(CASE WHEN status_code >= 400 THEN 1 END) as error_requests,
    DATE(created_at) as request_date
FROM request_logs
GROUP BY tenant_id, DATE(created_at);

-- ============================================
-- CLEANUP JOBS (Optional)
-- ============================================

-- Function to clean up expired tokens
CREATE OR REPLACE FUNCTION cleanup_expired_tokens()
RETURNS void AS $$
BEGIN
    DELETE FROM auth_tokens
    WHERE expires_at < NOW();
END;
$$ LANGUAGE plpgsql;

-- Function to clean up old request logs (older than 30 days)
CREATE OR REPLACE FUNCTION cleanup_old_logs()
RETURNS void AS $$
BEGIN
    DELETE FROM request_logs
    WHERE created_at < NOW() - INTERVAL '30 days';
END;
$$ LANGUAGE plpgsql;

-- ============================================
-- END OF SCHEMA
-- ============================================
