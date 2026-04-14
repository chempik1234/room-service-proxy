# RoomService Proxy - Deployment Guide

## 🚀 Complete Deployment Instructions

### Prerequisites
- Go 1.21+
- PostgreSQL 14+
- Railway account with payment method
- ~500MB free disk space (for Go modules)

---

## 📦 Step 1: Resolve Disk Space & Install Dependencies

```bash
# Clean up Go cache to free space
go clean -modcache

# Set GOPATH to a location with more space if needed
export GOPATH=/path/to/drive/with/more/space

# Install dependencies (when you have space)
cd room-service-proxy
go mod download
```

---

## 🔐 Step 2: Set Up Database

### Local Development:
```bash
# Create database
createdb roomservice_proxy

# Run schema
psql roomservice_proxy < schema.sql
```

### Railway PostgreSQL:
```bash
# Railway will provide DATABASE_URL
# Connect via Railway CLI:
railway connect
# In psql: \i schema.sql
```

---

## 🔑 Step 3: Configure Environment Variables

### Create `.env` file:
```bash
cp .env.example .env
```

### Edit `.env` with your values:
```bash
# Database (Railway provides this)
DATABASE_URL=postgresql://postgres:password@host.railway.app:5432/railway

# gRPC Server
GRPC_PORT=50051

# Rate Limiting
RATE_LIMIT_RPS=100
RATE_LIMIT_WINDOW=60s
RATE_LIMIT_BURST=10

# Admin API (generate secure key)
ADMIN_API_KEY=rs_admin_your_secure_random_key_here

# Feature Flags
ENABLE_AUTH=true
ENABLE_RATE_LIMIT=true

# Railway (for provisioning)
RAILWAY_TOKEN=your_railway_token
RAILWAY_PROJECT_ID=your_project_id
```

### Generate secure ADMIN_API_KEY:
```bash
# Generate 32-byte random key
openssl rand -base64 32
```

---

## 🐳 Step 4: Local Testing

### Run locally:
```bash
# Set environment variables
export $(cat .env | xargs)

# Run the service
go run main.go
```

### Test endpoints:
```bash
# Health check
curl http://localhost:8080/health

# Status
curl http://localhost:8080/status

# Create tenant (with admin key)
curl -X POST http://localhost:8080/api/tenants \
  -H "Authorization: Bearer rs_admin_your_key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Tenant",
    "email": "test@example.com",
    "plan": "free"
  }'
```

---

## 🚢 Step 5: Deploy to Railway

### Option A: Deploy via Railway CLI
```bash
# Install Railway CLI
npm install -g @railway/cli

# Login
railway login

# Create project
railway new --name roomservice-proxy

# Add PostgreSQL
railway add postgresql

# Get DATABASE_URL
railway variables list

# Update .env with Railway DATABASE_URL

# Set environment variables
railway variables set DATABASE_URL="postgresql://..."
railway variables set ADMIN_API_KEY="rs_admin_your_key"
railway variables set GRPC_PORT="50051"
railway variables set ADMIN_PORT="8080"
railway variables set RATE_LIMIT_RPS="100"
railway variables set ENABLE_AUTH="true"
railway variables set ENABLE_RATE_LIMIT="true"

# Initialize database
railway connect
# In psql: \i schema.sql

# Deploy
railway up
```

### Option B: Deploy via GitHub
```bash
# Push to GitHub
git add .
git commit -m "Initial commit"
git push

# In Railway dashboard:
# 1. Click "New Project"
# 2. Select "Deploy from GitHub repo"
# 3. Choose this repo
# 4. Add PostgreSQL service
# 5. Set environment variables
# 6. Deploy
```

---

## ✅ Step 6: Verify Deployment

### Check service health:
```bash
# Get your Railway service URL
railway domain

# Test health endpoint
curl https://your-service.up.railway.app/health

# Test status endpoint
curl https://your-service.up.railway.app/status
```

### Test tenant creation:
```bash
curl -X POST https://your-service.up.railway.app/api/tenants \
  -H "Authorization: Bearer YOUR_ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Acme Corp",
    "email": "contact@acme.com",
    "plan": "free"
  }'
```

---

## 🔧 Step 7: Configure Railway Payment

### Update payment method:
1. Go to railway.app
2. Click Settings > Billing
3. Add payment method
4. Verify monthly limit: **$10-20/month**

### Current monthly costs:
- Control plane: ~$10/month
  - Smart proxy: $5
  - PostgreSQL: $5 (or included)

---

## 🎯 Step 8: Test Complete Flow

### Create a test tenant:
```bash
curl -X POST https://your-proxy.up.railway.app/api/tenants \
  -H "Authorization: Bearer YOUR_ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test User",
    "email": "user@example.com",
    "plan": "free"
  }'
```

### Expected response:
```json
{
  "id": "tenant-testuser-abc123",
  "name": "Test User",
  "email": "user@example.com",
  "api_key": "rs_live_tenant-testuser-abc123_xyz789...",
  "host": "tenant-testuser-abc123.up.railway.app",
  "port": 50051,
  "status": "active",
  "plan": "free",
  "max_rooms": 50,
  "max_rps": 100,
  "created_at": "2024-01-15T10:30:00Z"
}
```

### Test proxy with API key:
```bash
# Test gRPC connection (when you have gRPC tools)
grpcurl -plaintext \
  -H "x-api-key: rs_live_tenant-testuser-abc123_xyz789..." \
  your-proxy.up.railway.app:50051 \
  list
```

---

## 🔐 Security Notes

### Password Generation:
✅ **Database passwords are randomly generated (32 chars)**
✅ **Stored securely in tenant_passwords table**
✅ **Only accessible via tenant ID + service type**

### API Keys:
✅ **Admin API key: Generated by you (32 chars recommended)**
✅ **Tenant API keys: Auto-generated (secure random)**
✅ **Regenerate API keys if compromised**

### Rate Limiting:
✅ **Per-tenant rate limiting**
✅ **Configurable RPS per plan**
✅ **Token bucket algorithm**

---

## 📊 Monitoring & Maintenance

### Check logs:
```bash
# Railway logs
railway logs

# Specific service
railway logs --service proxy
```

### Database queries:
```bash
# Connect to database
railway connect

# Check tenants
SELECT id, name, email, status, plan FROM tenants;

# Check recent requests
SELECT * FROM request_logs
WHERE created_at > NOW() - INTERVAL '1 hour'
ORDER BY created_at DESC
LIMIT 20;

# Check usage stats
SELECT * FROM usage_stats
WHERE stat_date = CURRENT_DATE;
```

---

## 🚨 Troubleshooting

### Issue: "Not enough disk space"
```bash
# Solution: Clean Go module cache
go clean -modcache

# Or change GOPATH
export GOPATH=/path/to/drive/with/more/space
```

### Issue: Database connection failed
```bash
# Check DATABASE_URL is correct
echo $DATABASE_URL

# Test connection
psql $DATABASE_URL
```

### Issue: Railway deployment failed
```bash
# Check logs
railway logs

# Redeploy
railway up
```

---

## 📈 Scaling

### When to scale up:
- 10+ Pro tenants: Consider dedicated database
- 100+ tenants: Multiple proxy instances
- 1000+ tenants: Separate proxy & database clusters

### Cost projection:
- 10 Pro tenants: ~$160/month ($10 control + $15×10)
- 100 Pro tenants: ~$1,510/month ($10 control + $15×100)
- Revenue at $49/tenant: $4,900/month
- Profit: $3,390/month

---

## ✅ Deployment Checklist

- [ ] Disk space freed up (500MB+)
- [ ] Go dependencies installed
- [ ] Database created and schema loaded
- [ ] Environment variables configured
- [ ] ADMIN_API_KEY generated (secure)
- [ ] Local testing successful
- [ ] Railway project created
- [ ] PostgreSQL service added
- [ ] Environment variables set in Railway
- [ ] Schema loaded into Railway database
- [ ] Service deployed to Railway
- [ ] Health check passing
- [ ] Tenant creation tested
- [ ] gRPC proxy tested
- [ ] Payment method configured

---

## 🎉 Next Steps

Once deployed:
1. **Test with real users** (5-10 beta testers)
2. **Monitor usage** (request logs, rate limits)
3. **Gather feedback** and improve
4. **Scale when needed** (upgrade plans)

**Your SaaS platform is live!** 🚀
