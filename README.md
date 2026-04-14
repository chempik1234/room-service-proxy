# RoomService Proxy

Smart proxy service for RoomService SaaS platform. Routes requests to tenant instances based on API keys with rate limiting and authentication.

## 🚀 Features

- **API Key Authentication**: Secure tenant identification
- **Rate Limiting**: Per-tenant request rate limiting (configurable RPS)
- **Tenant Management**: CRUD operations for tenant management
- **Request Proxying**: Routes gRPC requests to tenant instances
- **Admin API**: HTTP API for tenant management
- **Database**: PostgreSQL for tenant storage
- **Monitoring**: Request logging and usage statistics

## 📦 Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 14+
- Railway account (for tenant instances)

### Installation

1. **Clone the repository**
```bash
git clone https://github.com/chempik1234/room-service-proxy.git
cd room-service-proxy
```

2. **Install dependencies**
```bash
go mod download
```

3. **Set up database**
```bash
# Create database
createdb roomservice_proxy

# Run schema
psql roomservice_proxy < schema.sql
```

4. **Configure environment**
```bash
cp .env.example .env
# Edit .env with your configuration
```

5. **Run**
```bash
go run main.go
```

## ⚙️ Configuration

### Environment Variables

```bash
# Database
DATABASE_URL=postgresql://user:password@localhost:5432/roomservice_proxy

# gRPC Server
GRPC_PORT=50051

# Rate Limiting
RATE_LIMIT_RPS=100          # Requests per second per tenant
RATE_LIMIT_WINDOW=60s       # Time window
RATE_LIMIT_BURST=10         # Burst size

# Admin API
ADMIN_PORT=8080
ADMIN_API_KEY=your_secret_key

# Feature Flags
ENABLE_AUTH=true
ENABLE_RATE_LIMIT=true

# Railway (for auto-provisioning)
RAILWAY_TOKEN=your_railway_token
RAILWAY_PROJECT_ID=your_project_id
```

## 📊 API Endpoints

### Admin API (HTTP)

#### Create Tenant
```bash
POST /api/tenants
Authorization: Bearer your_admin_api_key
Content-Type: application/json

{
  "name": "Acme Corp",
  "email": "contact@acme.com",
  "host": "tenant-abc123.up.railway.app",
  "port": 50051,
  "plan": "free"
}

Response:
{
  "id": "tenant-abc123-xyz789",
  "name": "Acme Corp",
  "api_key": "rs_live_tenant-abc123-xyz789_***",
  "status": "active",
  "created_at": "2024-01-15T10:30:00Z"
}
```

#### List Tenants
```bash
GET /api/tenants
Authorization: Bearer your_admin_api_key

Response:
{
  "tenants": [
    {
      "id": "tenant-abc123-xyz789",
      "name": "Acme Corp",
      "email": "contact@acme.com",
      "status": "active",
      "plan": "free",
      "max_rooms": 50,
      "max_rps": 100
    }
  ]
}
```

#### Get Tenant
```bash
GET /api/tenants/:id
Authorization: Bearer your_admin_api_key
```

#### Update Tenant
```bash
PUT /api/tenants/:id
Authorization: Bearer your_admin_api_key
Content-Type: application/json

{
  "name": "Acme Corp Updated",
  "plan": "pro",
  "max_rooms": 500,
  "max_rps": 1000
}
```

#### Delete Tenant
```bash
DELETE /api/tenants/:id
Authorization: Bearer your_admin_api_key
```

#### Regenerate API Key
```bash
POST /api/tenants/:id/regenerate-api-key
Authorization: Bearer your_admin_api_key

Response:
{
  "api_key": "rs_live_tenant-abc123-xyz789_newkey"
}
```

### gRPC Proxy

#### Client Request
```go
// Client connects with API key
conn, _ := grpc.Dial("roomservice.io:50051",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithPerRPCCredentials(
        oauth.TokenSource{
            Token: "rs_live_tenant-abc123-xyz789_***",
        },
    },
)

client := room_service.NewRoomServiceClient(conn)

// Request is proxied to tenant instance
rooms, _ := client.RoomsList(ctx, &empty.Empty{})
```

#### How It Works
1. Client sends gRPC request with API key
2. Proxy validates API key against database
3. Proxy checks rate limits for tenant
4. Proxy forwards request to tenant's RoomService instance
5. Response is returned to client

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   RoomService Proxy                      │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌────────────────────────────────────────────────┐    │
│  │  gRPC Server (:50051)                          │    │
│  │  - Validates API keys                          │    │
│  │  - Checks rate limits                          │    │
│  │  - Routes to tenant instances                  │    │
│  └────────────────────────────────────────────────┘    │
│                                                          │
│  ┌────────────────────────────────────────────────┐    │
│  │  Admin API (:8080)                             │    │
│  │  - Create/Read/Update/Delete tenants           │    │
│  │  - Regenerate API keys                         │    │
│  │  - View usage stats                            │    │
│  └────────────────────────────────────────────────┘    │
│                                                          │
│  ┌────────────────────────────────────────────────┐    │
│  │  Database (PostgreSQL)                         │    │
│  │  - Tenants table                               │    │
│  │  - Request logs                                │    │
│  │  - Usage statistics                            │    │
│  └────────────────────────────────────────────────┘    │
│                                                          │
└─────────────────────────────────────────────────────────┘
                          │
                          │ Routes based on API key
                          ▼
┌─────────────────────────────────────────────────────────┐
│              Tenant Instances (Railway)                  │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │ Tenant A     │  │ Tenant B     │  │ Tenant C     │ │
│  │ RoomService  │  │ RoomService  │  │ RoomService  │ │
│  │ + MongoDB    │  │ + MongoDB    │  │ + MongoDB    │ │
│  │ + Redis      │  │ + Redis      │  │ + Redis      │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
└─────────────────────────────────────────────────────────┘
```

## 🔐 Security

- **API Key Authentication**: All requests require valid API key
- **Rate Limiting**: Per-tenant rate limiting prevents abuse
- **Admin API**: Separate admin API key for management operations
- **Database**: Parameterized queries prevent SQL injection
- **TLS**: Support for TLS encryption (configure in production)

## 📈 Monitoring

### Request Logging
All requests are logged to the database:
```sql
SELECT * FROM request_logs
WHERE tenant_id = 'tenant-abc123'
  AND created_at > NOW() - INTERVAL '1 hour';
```

### Usage Statistics
Tenant usage is tracked daily:
```sql
SELECT * FROM usage_stats
WHERE tenant_id = 'tenant-abc123'
  AND stat_date = CURRENT_DATE;
```

### Rate Limiting
Rate limiting stats per tenant:
```go
tokens, lastRefill, exists := limiter.GetStats("tenant-abc123")
```

## 🚀 Deployment

### Railway Deployment

1. **Create Railway project**
```bash
railway new --name roomservice-proxy
```

2. **Add PostgreSQL**
```bash
railway add postgresql
```

3. **Run schema**
```bash
railway connect
# In psql: \i schema.sql
```

4. **Set environment variables**
```bash
railway variables set DATABASE_URL=postgresql://...
railway variables set ADMIN_API_KEY=your_secret_key
railway variables set GRPC_PORT=50051
railway variables set ADMIN_PORT=8080
```

5. **Deploy**
```bash
railway up
```

### Docker Deployment

```bash
# Build image
docker build -t roomservice-proxy .

# Run container
docker run -d \
  -p 50051:50051 \
  -p 8080:8080 \
  -e DATABASE_URL=postgresql://... \
  -e ADMIN_API_KEY=your_secret_key \
  roomservice-proxy
```

## 🧪 Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific test
go test -v ./internal/proxy
```

## 📝 Development

### Project Structure
```
room-service-proxy/
├── main.go                 # Application entry point
├── internal/
│   ├── api/                # Admin API
│   ├── config/             # Configuration
│   ├── proxy/              # gRPC proxy logic
│   ├── ratelimit/          # Rate limiting
│   └── tenant/             # Tenant management
├── proto/                  # Protocol buffers
├── schema.sql              # Database schema
├── Dockerfile              # Docker image
├── Railway.toml            # Railway config
├── .env.example            # Environment template
└── README.md               # This file
```

### Adding Features

1. **New tenant field**: Update `Tenant` struct and database schema
2. **New rate limiting rule**: Update `ratelimit` package
3. **New admin endpoint**: Add to `api` package
4. **New proxy logic**: Update `proxy` package

## 🤝 Contributing

1. Fork the repository
2. Create feature branch
3. Commit changes
4. Push to branch
5. Open Pull Request

## 📄 License

MIT License - see LICENSE file for details

## 🙏 Acknowledgments

- [Railway](https://railway.app) - Easy deployment
- [gRPC](https://grpc.io/) - RPC framework
- [PostgreSQL](https://www.postgresql.org/) - Database
- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver

---

**Built with ❤️ for the RoomService SaaS platform**
