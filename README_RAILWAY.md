# Railway Port Configuration Solution

## Problem
Railway only allows **1 public port per domain**, but we need both:
- Port 8080 (HTTP Admin API)
- Port 50051 (gRPC)

## Solutions

### ✅ Solution 1: Use HTTP-only mode (Recommended)

**Deploy your proxy to handle both Admin API and gRPC over HTTP on port 8080:**

#### For Railway Deployment:
```yaml
# railway.toml
[deploy]
startCommand = " ./proxy-http"
healthcheckPath = "/health"
healthcheckTimeout = 300

[build]
builder = "DOCKERFILE"
dockerfilePath = "Dockerfile"

[[services]]
name = "room-service-proxy"
      protocol = "HTTP"
      targetPort = 8080
```

#### Client Configuration:
```javascript
const client = new RoomServiceClient({
  host: 'https://your-service.up.railway.app', // Just use port 8080
  apiKey: 'rs_live_yourtenantid_uuid',
  useHTTP: true // Enable HTTP mode
});
```

### ✅ Solution 2: Two Separate Railway Services

**Create 2 separate Railway services:**

1. **room-service-proxy** (HTTP) - Public on port 8080
2. **room-service-grpc** (gRPC) - Public on port 50051

Then connect:
```javascript
// For HTTP Admin API
const proxyClient = new ProxyClient({
  host: 'https://room-service-proxy.up.railway.app'
});

// For gRPC calls
const grpcClient = new RoomServiceClient({
  host: 'room-service-grpc.up.railway.app:50051',
  apiKey: 'rs_live_yourtenantid_uuid'
});
```

### ✅ Solution 3: Use Railway Private Networking

**Keep services separate but connect internally:**

1. Deploy HTTP proxy as public service (port 8080)
2. Deploy gRPC backend as private service (port 50051)
3. Configure HTTP proxy to forward gRPC calls internally

```go
// In your proxy service
backendGRPC := "room-service-grpc.railway.internal:50051"
conn, _ := grpc.Dial(backendGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
```

## Recommended Setup

### For Development (Local):
- HTTP: localhost:8080
- gRPC: localhost:50051

### For Production (Railway):
- HTTP: https://room-service-proxy.up.railway.app:8080
- gRPC: Use HTTP wrapper or separate service

## Client SDK Auto-Detection

The SDK can auto-detect which transport to use:

```javascript
const client = new RoomServiceClient({
  host: 'https://room-service-proxy.up.railway.app:8080',
  apiKey: 'rs_live_yourtenantid_uuid'
});

// SDK automatically detects:
// - Port 8080/443 → Use HTTP transport
// - Port 50051 → Use gRPC transport
```

## Quick Fix for Current Issue

**Update your Railway service to use HTTP-only mode:**

1. Change Railway port to: 8080
2. Update proxy to handle both HTTP and gRPC-Web
3. Update SDK to use HTTP transport for Railway URLs

This way you only need one public port! 🎉