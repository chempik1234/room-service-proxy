# RoomService Proxy

Smart multi-tenant proxy service for RoomService SaaS platform. Routes requests to tenant instances based on API keys with rate limiting, authentication, and support for multiple deployment providers.

## 🚀 Features

- **Multi-Provider Deployment**: Support for Railway, Yandex Cloud, and Docker
- **Unified Architecture**: Clean ports/adapters pattern for storage and deployment
- **API Key Authentication**: Secure tenant identification
- **Rate Limiting**: Per-tenant request rate limiting (configurable RPS)
- **Tenant Management**: CRUD operations for tenant management
- **Request Proxying**: Routes gRPC requests to tenant instances
- **Admin API**: HTTP API for tenant management
- **Automated Deployment**: GitHub Actions + Taskfile for CI/CD
- **Docker Compose**: Easy local development and production deployment

## 📦 Quick Start

### Prerequisites

- Go 1.25+ (for local development)
- Docker & Docker Compose
- Task (taskfile.dev)
- Deployment provider account (Railway/Yandex Cloud)

### Quick Deployment (Docker Compose)

```bash
# Clone the repository
git clone https://github.com/chempik1234/room-service-proxy.git
cd room-service-proxy

# Install Task
sudo sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

# Deploy everything
task deploy

# Check status
task status
```

### Manual Deployment

```bash
# Set up environment
cp .env.example .env
# Edit .env with your configuration

# Build and run
docker-compose up -d

# Or run locally
go mod download
go run cmd/proxy/main.go
```

## ⚙️ Configuration

### Environment Variables

```bash
# Database (Required)
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

# Deployment Provider (Required: "railway", "yandex", or "docker")
DEPLOYMENT_PROVIDER=docker

# Yandex Cloud (if using Yandex provider)
YANDEX_FOLDER_ID=your_folder_id
YANDEX_ZONE=ru-central1-a
YANDEX_SERVICE_ACCOUNT_KEY=/path/to/key.json
YANDEX_SSH_KEY_PATH=/path/to/ssh/key

# Railway (if using Railway provider)
RAILWAY_TOKEN=your_railway_token
RAILWAY_PROJECT_ID=your_project_id
RAILWAY_ENVIRONMENT_ID=your_environment_id
```

## 🏗️ Architecture

### Unified Ports & Adapters

```
┌─────────────────────────────────────────────────────────┐
│                   RoomService Proxy                      │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌────────────────────────────────────────────────┐    │
│  │  Tenant Service (Business Logic)              │    │
│  │  - Uses TenantStorage interface               │    │
│  │  - Uses ServiceDeployer interface             │    │
│  └────────────────────────────────────────────────┘    │
│           │                    │                        │
│           ▼                    ▼                        │
│  ┌──────────────────┐  ┌──────────────────┐           │
│  │  Storage Port    │  │  Deployer Port   │           │
│  └──────────────────┘  └──────────────────┘           │
│           │                    │                        │
│           ▼                    ▼                        │
│  ┌──────────────────┐  ┌──────────────────┐           │
│  │  PostgreSQL     │  │  Railway/Yandex  │           │
│  │  Adapter        │  │  Adapters        │           │
│  └──────────────────┘  └──────────────────┘           │
│                                                          │
└─────────────────────────────────────────────────────────┘
                          │
                          │ Routes based on API key
                          ▼
┌─────────────────────────────────────────────────────────┐
│              Tenant Instances (Multi-Provider)           │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │ Railway      │  │ Yandex       │  │ Docker       │ │
│  │ Containers   │  │ Compute VMs  │  │ Containers   │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### Project Structure

```
room-service-proxy/
├── cmd/proxy/              # Application entry point
├── internal/
│   ├── config/             # Configuration management
│   ├── dto/                # Data transfer objects
│   ├── ports/              # Port interfaces
│   │   ├── storage.go      # TenantStorage interface
│   │   └── adapters/       # Storage & deployment adapters
│   │       ├── postgres_tenant_storage.go
│   │       ├── yandex_adapter.go
│   │       └── railway_adapter.go
│   ├── service/            # gRPC proxy logic
│   ├── tenant/             # Tenant management
│   │   ├── service.go      # Tenant service
│   │   └── factory.go      # Service factory functions
│   └── transport/http/     # Admin API
├── docs/                   # Documentation
│   ├── yandex-deployment.md
│   └── diagrams/
├── .github/                # GitHub Actions workflows
├── Dockerfile              # Docker image
├── docker-compose.yml      # Multi-container setup
├── Taskfile.yml            # Task automation
├── .env.example            # Environment template
└── README.md               # This file
```

## 📊 API Endpoints

### Admin API (HTTP:8080)

#### Create Tenant
```bash
POST /api/tenants
Authorization: Bearer your_admin_api_key
Content-Type: application/json

{
  "name": "Acme Corp",
  "email": "contact@acme.com",
  "plan": "pro"
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
      "plan": "pro"
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
  "plan": "enterprise"
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

### gRPC Proxy (Port 50051)

#### Client Request
```go
// Client connects with API key
conn, _ := grpc.Dial("roomservice.io:50051",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithPerRPCCredentials(
        oauth.TokenSource{
            Token: "rs_live_tenant-abc123-xyz789_***",
        },
    ),
)

client := room_service.NewRoomServiceClient(conn)

// Request is proxied to tenant instance
rooms, _ := client.RoomsList(ctx, &empty.Empty{})
```

## 🚀 Deployment

### Local Development (Docker Compose)

```bash
# Install Task
curl -sL https://taskfile.dev/install | sh

# Deploy everything
task deploy

# Useful commands
task status        # Check service health
task logs          # View logs
task rebuild       # Rebuild and restart
task test          # Test API endpoints
```

### Railway Deployment

```bash
# Configure for Railway
cp .env.example .env
# Edit .env with DEPLOYMENT_PROVIDER=railway

# Deploy
docker-compose up -d

# Or use Railway CLI
railway up
```

### Yandex Cloud Deployment

See [docs/yandex-deployment.md](docs/yandex-deployment.md) for detailed Yandex Cloud deployment instructions.

```bash
# Configure for Yandex
export DEPLOYMENT_PROVIDER=yandex
export YANDEX_FOLDER_ID=your_folder_id
export YANDEX_ZONE=ru-central1-a

# Deploy
task deploy
```

### Automated Deployment (GitHub Actions)

Every push to `master` triggers automated deployment:

1. **Build** Docker image
2. **Deploy** to configured VM
3. **Health Check** the deployment
4. **Notifications** on success/failure

**Required GitHub Secrets:**
```
YANDEK_HOST = your_vm_ip
YANDEK_USER = your_username
YANDEK_SSH_KEY = your_ssh_private_key
```

## 🔐 Security

- **API Key Authentication**: All requests require valid API key
- **Rate Limiting**: Per-tenant rate limiting prevents abuse
- **Admin API**: Separate admin API key for management operations
- **Clean Architecture**: Type-safe interfaces prevent data leakage
- **Multi-Tenancy**: Complete tenant isolation
- **TLS**: Support for TLS encryption (configure in production)

## 🧪 Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...

# Test API endpoints
task test

# Test specific service
go test -v ./internal/service
```

## 🛠️ Development

### Task Commands

```bash
task deploy        # Full deployment
task build         # Build Docker images
task up            # Start services
task down          # Stop services
task logs          # View logs
task status        # Check health
task rebuild       # Rebuild and restart
task clean         # Remove everything
```

### Adding Features

1. **New deployment provider**: 
   - Implement `ServiceDeployer` interface in `internal/ports/adapters/`
   - Add factory function in `internal/tenant/factory.go`
   - Update `internal/config/config.go`

2. **New storage backend**:
   - Implement `TenantStorage` interface
   - Add to factory functions

3. **New admin endpoint**:
   - Add to `internal/transport/http/admin.go`

## 📈 Monitoring

### Health Check
```bash
curl http://localhost:8080/health
```

### Service Status
```bash
task status
```

### Container Logs
```bash
task logs
task logs:proxy
task logs:postgres
```

## 🔧 Troubleshooting

### Docker Issues
```bash
# Check Docker version
docker --version

# Update Docker (if too old)
sudo apt update && sudo apt install -y docker-ce
```

### Permission Issues
```bash
# Add user to docker group
sudo usermod -aG docker $USER

# Re-login to apply changes
```

### Deployment Issues
```bash
# Check logs
task logs

# Rebuild from scratch
task rebuild

# Clean and restart
task clean && task deploy
```

## 🤝 Contributing

1. Fork the repository
2. Create feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push to branch (`git push origin feature/amazing-feature`)
5. Open Pull Request

## 📄 License

MIT License - see LICENSE file for details

## 🙏 Acknowledgments

- [Railway](https://railway.app) - Container platform
- [Yandex Cloud](https://cloud.yandex.com/) - Compute platform
- [gRPC](https://grpc.io/) - RPC framework
- [PostgreSQL](https://www.postgresql.org/) - Database
- [Docker](https://www.docker.com/) - Containerization
- [Task](https://taskfile.dev/) - Task runner

---

**Built with ❤️ for the RoomService SaaS platform**
