# Tenant Instance Provisioning - Complete Setup

## 🎯 Goal
When a user creates a tenant via the control panel, automatically provision a complete Railway deployment with:
- MongoDB database
- Redis cache
- RoomService instance

## 📋 Prerequisites Checklist

- [ ] Railway account with API token
- [ ] Docker Hub account or Railway Container Registry
- [ ] RoomService Docker image pushed to registry
- [ ] Railway token configured in proxy environment

## 🔧 Setup Steps

### 1. Get Railway API Token

```bash
# Login to Railway and get your token from:
# https://backboard.railway.app/account/api

export RAILWAY_TOKEN="your_token_here"
```

### 2. Build and Push RoomService Docker Image

#### Option A: Push to Docker Hub
```bash
# Build RoomService image
cd /path/to/RoomService
docker build -f docker/service.Dockerfile -t yourusername/roomservice:latest .

# Push to Docker Hub
docker push yourusername/roomservice:latest
```

#### Option B: Use Railway's Registry
```bash
# Login to Railway registry
docker loginup.railway.app

# Build and tag for Railway
docker build -f docker/service.Dockerfile -t up.railway.app/yourusername/roomservice:latest .

# Push to Railway
docker push up.railway.app/yourusername/roomservice:latest
```

### 3. Update Railway Service Creation

The current implementation uses placeholder images. Update `railway.go`:

```go
// CreateRoomService creates a RoomService
func (r *RailwayService) CreateRoomService(projectID, tenantID, mongoURL, redisURL string) (*RoomServiceInfo, error) {
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			mutation($projectId: String!, $name: String!) {
				serviceCreate(
					projectId: $projectId,
					name: $name,
					image: "yourusername/roomservice:latest"  // UPDATE THIS
				) {
					id
				}
			}
		`),
		"variables": map[string]interface{}{
			"projectId": projectID,
			"name":      tenantID,
		},
	}

	// ... rest of the function
}
```

### 4. Configure Proxy Environment

```bash
# In your Railway proxy service or .env file:
RAILWAY_TOKEN=your_railway_token_here
DATABASE_URL=your_postgres_connection_string
ADMIN_API_KEY=your_admin_api_key
```

### 5. Test Tenant Provisioning

```bash
# Create a test tenant via the API
curl -X POST https://your-proxy.up.railway.app/api/tenants \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Tenant",
    "email": "test@example.com",
    "plan": "free"
  }'

# Expected response:
{
  "id": "tenant-id",
  "name": "Test Tenant",
  "host": "tenant-project-id-tenant-id.up.railway.app",
  "port": 50051,
  "status": "provisioning"
}
```

### 6. Verify Provisioned Tenant

After ~2-3 minutes, check Railway dashboard for:
- ✅ New project created
- ✅ MongoDB service running
- ✅ Redis service running
- ✅ RoomService service running

## 🏗️ Architecture

```
User creates tenant
    ↓
Control Panel → Proxy API
    ↓
Proxy calls Railway API
    ↓
Railway provisions:
├── New Project
├── MongoDB (with random password)
├── Redis (with random password)
└── RoomService (from Docker image)
    ↓
Proxy stores tenant connection details
    ↓
SDK requests routed to tenant's Railway deployment
```

## 🔍 Current Issues to Fix

### 1. Docker Image Reference
**Problem**: `railway.go` uses placeholder image
**Fix**: Update to use actual RoomService image

### 2. Service URL Detection
**Problem**: `getServiceURL()` returns placeholder URLs
**Fix**: Implement proper Railway domain detection

### 3. Health Check Timeout
**Problem**: 5-minute timeout might be too short
**Fix**: Add better status tracking and callbacks

## 🚀 Alternative Approaches

### Option A: Railway Services (Current Plan)
- **Pros**: True multi-tenancy, isolated environments
- **Cons**: Expensive ($5-20/month per tenant), slow provisioning

### Option B: Shared Instance with Database Isolation
- **Pros**: Cheap, fast provisioning
- **Cons**: Less isolation, shared resources

### Option C: Docker Container per Tenant
- **Pros**: Good isolation, self-hosted
- **Cons**: Complex infrastructure management

## 🎯 Immediate Next Steps

1. **Push RoomService image** to Docker Hub/Railway registry
2. **Update Railway token** in proxy configuration
3. **Fix Docker image reference** in `railway.go`
4. **Test tenant creation** via control panel
5. **Monitor Railway dashboard** for provisioned services

## 📊 Cost Estimation

Per tenant (Railway):
- MongoDB: ~$5/month
- Redis: ~$5/month
- RoomService: ~$5/month
- **Total**: ~$15/month per tenant

For 10 free tier tenants: **$150/month**

## 💡 Money-Saving Alternatives

1. **Use shared instances** with tenant ID routing
2. **Serverless functions** for tenant isolation
3. **Regional grouping** - multiple tenants per Railway project