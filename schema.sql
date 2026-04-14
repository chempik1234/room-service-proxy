-- RoomService Proxy Database Schema

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

-- Create tenants table
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE, -- Track tenant ownership
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

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_tenants_api_key ON tenants(api_key);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenants_plan ON tenants(plan);
CREATE INDEX IF NOT EXISTS idx_tenants_created_at ON tenants(created_at);

-- Create request_logs table for monitoring
CREATE TABLE IF NOT EXISTS request_logs (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    method TEXT,
    path TEXT,
    status_code INTEGER,
    latency_ms INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create index for request logs
CREATE INDEX IF NOT EXISTS idx_request_logs_tenant_id ON request_logs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at);

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

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger to auto-update updated_at
DROP TRIGGER IF EXISTS update_tenants_updated_at ON tenants;
CREATE TRIGGER update_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

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

-- Insert sample data (optional)
-- INSERT INTO tenants (name, email, api_key, host, port, status, plan)
-- VALUES ('Test Tenant', 'test@example.com', 'rs_live_test_key', 'localhost', 50051, 'active', 'free');
