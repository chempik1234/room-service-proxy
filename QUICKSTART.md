# 🚀 Quick Start: Tenant Provisioning Setup

## Step 1: Get Railway API Token
```bash
# 1. Go to: https://backboard.railway.app/account/api
# 2. Copy your API token
# 3. Set environment variable:
export RAILWAY_TOKEN="your_railway_token_here"
```

## Step 2: Build and Push RoomService Image
```bash
# From RoomService directory:
cd /path/to/RoomService

# Make build script executable
chmod +x build-and-push.sh

# Build and push to Docker Hub (default)
./build-and-push.sh

# Or push to Railway registry:
REGISTRY=up.railway.app ./build-and-push.sh
```

## Step 3: Update Railway Configuration
```bash
# In room-service-proxy, update railway.go:
# Change line 136:
dockerImage := "yourusername/roomservice:latest"  # Your actual image
```

## Step 4: Configure Proxy Environment
```bash
# Add to your Railway proxy service environment variables:
RAILWAY_TOKEN=your_token_here
DATABASE_URL=postgres://...
ADMIN_API_KEY=your_admin_key
```

## Step 5: Test Tenant Creation
```bash
# Create a test tenant:
curl -X POST http://localhost:8080/api/tenants \
  -H "Authorization: Bearer test_admin_key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My First Tenant",
    "email": "user@example.com",
    "plan": "free"
  }'
```

## Expected Result:
```json
{
  "id": "tenant-abc123",
  "name": "My First Tenant",
  "email": "user@example.com",
  "plan": "free",
  "status": "provisioning",
  "host": "project-id-tenant-id.up.railway.app",
  "port": 50051
}
```

## 🔍 Monitor Provisioning:
1. Check Railway Dashboard for new project
2. Wait 2-3 minutes for services to start
3. Verify tenant status in proxy database
4. Test connection with SDK

## 🎯 Success Criteria:
- ✅ New Railway project created
- ✅ MongoDB, Redis, RoomService services running
- ✅ Tenant record in database with correct host/port
- ✅ SDK can connect to tenant instance

## 💰 Cost Warning:
Each tenant costs ~$15/month on Railway ($5 × 3 services)
Consider using shared instances for production!