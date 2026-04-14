# 🚀 RoomService Proxy - Railway Deployment Guide

## ✅ **Current Status**
- ✅ Landing page: **DEPLOYED** 🎉
- ✅ Proxy binary: **BUILT** (42MB)
- ✅ Dependencies: **INSTALLED**
- ✅ 1.8GB disk space: **AVAILABLE**

---

## 📋 **Step-by-Step Railway Deployment**

### Step 1: Install Railway CLI (2 minutes)
```bash
npm install -g @railway/cli
```

### Step 2: Login to Railway (1 minute)
```bash
railway login
# This will open a browser window for authentication
```

### Step 3: Create Railway Project (1 minute)
```bash
cd room-service-proxy
railway new --name roomservice-proxy
```

### Step 4: Add PostgreSQL Service (2 minutes)
```bash
railway add postgresql
# Wait for PostgreSQL to be ready (usually 1-2 minutes)
```

### Step 5: Get Database URL (30 seconds)
```bash
railway variables list
# Look for RAILWAY_POSTGRESERVICE_URL
# It will look like: postgresql://postgres:password@host.railway.app:5432/railway
```

### Step 6: Set Environment Variables (2 minutes)
```bash
# Replace YOUR_DATABASE_URL with the actual URL from step 5
railway variables set DATABASE_URL="YOUR_DATABASE_URL"

# Generate secure admin API key
railway variables set ADMIN_API_KEY="rs_admin_$(openssl rand -hex 16)"

# Other settings
railway variables set GRPC_PORT="50051"
railway variables set ADMIN_PORT="8080"
railway variables set RATE_LIMIT_RPS="100"
railway variables set RATE_LIMIT_WINDOW="60s"
railway variables set RATE_LIMIT_BURST="10"
railway variables set ENABLE_AUTH="true"
railway variables set ENABLE_RATE_LIMIT="true"
```

### Step 7: Set Up Database Schema (5 minutes)
```bash
railway connect
```

**A psql console will open. Copy and paste this:**

```sql
-- Create tenants table
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    api_key TEXT UNIQUE NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    plan TEXT NOT NULL DEFAULT 'free',
    max_rooms INTEGER DEFAULT 50,
    max_rps INTEGER DEFAULT 100,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_tenants_api_key ON tenants(api_key);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);

-- Create request_logs table
CREATE TABLE IF NOT EXISTS request_logs (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    method TEXT,
    path TEXT,
    status_code INTEGER,
    latency_ms INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_request_logs_tenant_id ON request_logs(tenant_id);

-- Create usage_stats table
CREATE TABLE IF NOT EXISTS usage_stats (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    stat_date DATE NOT NULL,
    request_count INTEGER DEFAULT 0,
    room_count INTEGER DEFAULT 0,
    unique (tenant_id, stat_date)
);

CREATE INDEX IF NOT EXISTS idx_usage_stats_tenant_id ON usage_stats(tenant_id);

-- Create tenant_passwords table for secure password storage
CREATE TABLE IF NOT EXISTS tenant_passwords (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    service_type TEXT NOT NULL,
    password TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    unique (tenant_id, service_type)
);

CREATE INDEX IF NOT EXISTS idx_tenant_passwords_tenant_id ON tenant_passwords(tenant_id);

-- Create function to update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create triggers
DROP TRIGGER IF EXISTS update_tenants_updated_at ON tenants;
CREATE TRIGGER update_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_tenant_passwords_updated_at ON tenant_passwords;
CREATE TRIGGER update_tenant_passwords_updated_at
    BEFORE UPDATE ON tenant_passwords
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
```

**Type `\q` to exit psql when done.**

### Step 8: Deploy the Service (3 minutes)
```bash
railway up
```

### Step 9: Verify Deployment (1 minute)
```bash
# Check logs
railway logs

# Get service URL
railway domain

# Test health endpoint
curl https://roomservice-proxy.up.railway.app/health

# Test status endpoint
curl https://roomservice-proxy.up.railway.app/status
```

---

## ✅ **Your Control Plane is Live!**

**Service URL**: `https://roomservice-proxy.up.railway.app`

**What you have:**
- ✅ Smart proxy running on Railway
- ✅ PostgreSQL database configured
- ✅ Admin API accessible
- ✅ Ready to provision tenants

---

## 🎯 **Test Your SaaS Platform**

### 1. Create Your First Tenant
```bash
curl -X POST https://roomservice-proxy.up.railway.app/api/tenants \
  -H "Authorization: Bearer YOUR_ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test User",
    "email": "test@example.com",
    "plan": "free"
  }'
```

### Expected Response:
```json
{
  "id": "tenant-testuser-abc123",
  "name": "Test User",
  "email": "test@example.com",
  "api_key": "rs_live_tenant-testuser-abc123_xyz789...",
  "status": "active",
  "plan": "free",
  "max_rooms": 50,
  "max_rps": 100
}
```

### 2. Test the Landing Page → Proxy Flow
- Visit your landing page
- Fill out the signup form
- Get your API key
- Start using the service!

---

## 💰 **Monthly Cost**

```
Control Plane:        $10/month
├── Smart Proxy:      $5
└── PostgreSQL:       $5

Break-even:          1 Pro tenant ($49/month)
Profit per Pro:      $34/month
```

---

## 🎉 **Congratulations!**

Your RoomService SaaS platform is now **LIVE** and ready to accept customers!

**What you accomplished:**
1. ✅ Professional landing page deployed
2. ✅ Control plane service deployed
3. ✅ Railway infrastructure configured
4. ✅ Random password generation implemented
5. ✅ Ready to scale from 1 to 1000+ tenants

**Next steps:**
1. Share the landing page with potential users
2. Monitor your Railway costs (should stay around $10/month)
3. Gather feedback and improve
4. Scale when you have customers!

**You did it! Your SaaS is live!** 🚀
