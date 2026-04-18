# Room Service Proxy Architecture

## Overview

The Room Service Proxy is a multi-tenant SaaS wrapper that provides isolation, routing, and management for multiple RoomService instances. Each tenant gets their own isolated RoomService deployment while sharing the proxy infrastructure.

## High-Level Architecture

```mermaid
graph TB
    subgraph "Room Service Proxy"
        AdminAPI[Admin API<br/>:8080]
        GRPCProxy[gRPC Proxy<br/>:50051]
        TenantService[Tenant Management<br/>Service]
        PostgreSQL[(PostgreSQL<br/>Database)]
        
        AdminAPI --> PostgreSQL
        TenantService --> PostgreSQL
        GRPCProxy --> TenantService
    end
    
    subgraph "Tenant 1 VM"
        VM1Room[RoomService<br/>:50050]
        VM1Mongo[(MongoDB<br/>:27017)]
        VM1Redis[(Redis<br/>:6379)]
        
        VM1Room --> VM1Mongo
        VM1Room --> VM1Redis
    end
    
    subgraph "Tenant 2 VM"
        VM2Room[RoomService<br/>:50050]
        VM2Mongo[(MongoDB<br/>:27017)]
        VM2Redis[(Redis<br/>:6379)]
        
        VM2Room --> VM2Mongo
        VM2Room --> VM2Redis
    end
    
    subgraph "Tenant 3 VM"
        VM3Room[RoomService<br/>:50050]
        VM3Mongo[(MongoDB<br/>:27017)]
        VM3Redis[(Redis<br/>:6379)]
        
        VM3Room --> VM3Mongo
        VM3Room --> VM3Redis
    end
    
    GRPCProxy --> VM1Room
    GRPCProxy --> VM2Room
    GRPCProxy --> VM3Room
    
    style AdminAPI fill:#e1f5ff
    style GRPCProxy fill:#fff4e1
    style TenantService fill:#e8f5e8
    style PostgreSQL fill:#f3e5f5
    style VM1Room fill:#ffe1e1
    style VM2Room fill:#ffe1e1
    style VM3Room fill:#ffe1e1
    style VM1Mongo fill:#e1f5ff
    style VM2Mongo fill:#e1f5ff
    style VM3Mongo fill:#e1f5ff
    style VM1Redis fill:#fff9e1
    style VM2Redis fill:#fff9e1
    style VM3Redis fill:#fff9e1
```

## Request Flow

### 1. Tenant Creation Flow

```mermaid
sequenceDiagram
    participant Client
    participant AdminAPI as Admin API
    participant TenantService as Tenant Service
    participant DB as PostgreSQL
    participant Cloud as Cloud Provider
    participant VM as Tenant VM
    
    Client->>AdminAPI: POST /api/tenants
    AdminAPI->>TenantService: CreateTenantWithProvisioning()
    TenantService->>DB: INSERT tenant record
    TenantService->>Cloud: Deploy VM with docker-compose
    Cloud-->>TenantService: VM provisioning started
    TenantService->>VM: Wait for health check
    VM-->>TenantService: Services healthy
    TenantService->>DB: UPDATE tenant (host, port, status)
    TenantService-->>AdminAPI: Tenant created
    AdminAPI-->>Client: 201 Created + tenant details
    
    Note over VM,Cloud: Takes 30-60 seconds
```

### 2. gRPC Request Flow (Runtime)

```mermaid
sequenceDiagram
    participant Client
    participant Proxy as gRPC Proxy (Binary)
    participant Auth as Auth Interceptor
    participant RateLimit as Rate Limiter
    participant Director as Request Director
    participant Pool as Connection Pool
    participant VM as Tenant VM (Docker Host)
    
    Client->>Proxy: gRPC Request + API Key
    
    Proxy->>Proxy: Request ID Middleware<br/>Generate UUID
    
    Proxy->>Auth: Validate API Key
    Auth->>Proxy: Tenant Info
    
    Proxy->>RateLimit: Check tenant limits
    RateLimit->>Proxy: Allow/Deny
    
    Proxy->>Director: Route to tenant VM
    Director->>Pool: Get/Create connection
    Pool-->>Director: gRPC connection
    Director->>VM: Forward request to container
    
    VM-->>Director: Response from container
    Director-->>Proxy: Response
    Proxy-->>Client: gRPC Response
    
    Note over VM,VM: All services run as Docker containers
```

## Authentication & Routing Flow

```mermaid
flowchart TD
    Start([Client Request]) --> AuthKey{Has API Key?}
    AuthKey -->|No| AuthFail[Return 401 Unauthorized]
    AuthKey -->|Yes| ValidateKey[Validate API Key]
    
    ValidateKey --> KeyValid{Key Valid?}
    KeyValid -->|No| AuthFail
    KeyValid -->|Yes| CheckStatus{Tenant Active?}
    
    CheckStatus -->|No| StatusFail[Return 403 Forbidden]
    CheckStatus -->|Yes| RateLimit[Check Rate Limit]
    
    RateLimit --> LimitOK{Within Limit?}
    LimitOK -->|No| LimitFail[Return 429 Rate Limited]
    LimitOK -->|Yes| GetConnection[Get Tenant Connection]
    
    GetConnection --> PoolCheck{Connection Exists?}
    PoolCheck -->|Yes| Reuse[Reuse Connection]
    PoolCheck -->|No| Create[Create New Connection]
    
    Reuse --> Route[Route to Tenant VM]
    Create --> Route
    
    Route --> TenantVM[Tenant VM:50050]
    TenantVM --> Success([Return Response])
    
    style Start fill:#e8f5e8
    style Success fill:#e8f5e8
    style AuthFail fill:#ffebee
    style StatusFail fill:#ffebee
    style LimitFail fill:#ffebee
    style TenantVM fill:#e1f5ff
```

## Connection Pool Management

```mermaid
graph LR
    subgraph "Connection Pool"
        Pool1[tenant_123<br/>10.0.1.5:50051]
        Pool2[tenant_456<br/>10.0.2.10:50051]
        Pool3[tenant_789<br/>10.0.3.15:50051]
    end
    
    subgraph "Tenant VMs"
        VM1[VM1<br/>tenant_123]
        VM2[VM2<br/>tenant_456]
        VM3[VM3<br/>tenant_789]
    end
    
    Request1[Request 1<br/>tenant_123] --> Pool1
    Request2[Request 2<br/>tenant_456] --> Pool2
    Request3[Request 3<br/>tenant_789] --> Pool3
    
    Pool1 --> VM1
    Pool2 --> VM2
    Pool3 --> VM3
    
    style Pool1 fill:#e1f5ff
    style Pool2 fill:#e1f5ff
    style Pool3 fill:#e1f5ff
    style VM1 fill:#ffe1e1
    style VM2 fill:#ffe1e1
    style VM3 fill:#ffe1e1
```

## Tenant VM Architecture

Each tenant VM runs **all services as Docker containers** via docker-compose:

```mermaid
graph TB
    subgraph "Tenant VM (Docker Host)"
        Docker[Docker Engine<br/>+ docker-compose]
        
        subgraph "Docker Compose Project"
            RoomService[RoomService Container<br/>chempik1234/roomservice:latest<br/>:50050]
            MongoDB[MongoDB Container<br/>mongo:latest<br/>:27017]
            Redis[Redis Container<br/>redis:latest<br/>:6379]
            
            RoomService -->|MongoDB Driver| MongoDB
            RoomService -->|Redis Client| Redis
            
            MongoVol[mongo_data<br/>Docker Volume]
            RedisVol[redis_data<br/>Docker Volume]
            
            MongoDB --> MongoVol
            Redis --> RedisVol
        end
        
        subgraph "Networking"
            External[External Access<br/>50051:50050]
            Network[Docker Network<br/>backend]
            
            External --> RoomService
            RoomService --> Network
            MongoDB --> Network
            Redis --> Network
        end
    end
    
    style Docker fill:#e1f5ff
    style RoomService fill:#ffe1e1
    style MongoDB fill:#e8f5e8
    style Redis fill:#fff9e1
    style MongoVol fill:#c8e6c9
    style RedisVol fill:#c8e6c9
    style External fill:#f3e5f5
    style Network fill:#fce4ec
```

## Deployment Adapter Pattern

The proxy uses a **deployment adapter pattern** to support multiple cloud providers through a unified interface:

```mermaid
classDiagram
    class ServiceDeployer {
        <<interface>>
        +DeployTenant(ctx, tenantID, config) TenantDeployment
        +DeployDatabase(ctx, tenantID) DatabaseDeployment
        +DeployCache(ctx, tenantID) CacheDeployment
        +DeployApplication(ctx, tenantID, config) ApplicationDeployment
        +DeleteServices(ctx, tenantID) error
        +CheckHealth(ctx, tenantID) bool
        +GetStatus(ctx, tenantID) DeploymentStatus
    }
    
    class YandexServiceDeployer {
        +DeployTenant() TenantDeployment
        -createComputeInstanceWithConfig()
        -createDockerComposeConfig()
        -waitForTenantServices()
    }
    
    class RailwayServiceDeployer {
        +DeployTenant() TenantDeployment
        -CreateMongoDB()
        -CreateRedis()
        -CreateRoomService()
    }
    
    ServiceDeployer <|-- YandexServiceDeployer
    ServiceDeployer <|-- RailwayServiceDeployer
```

### Benefits of Adapter Pattern
- **Cloud Agnostic**: Easy to add new providers (AWS, GCP, Azure)
- **Unified API**: Single interface for all deployment operations
- **Provider Optimization**: Each adapter uses provider-specific best practices
- **Testing**: Mock adapters for development/testing
- **Migration**: Easy tenant migration between providers

## Deployment Providers Comparison

```mermaid
graph LR
    subgraph "Yandex Cloud (Recommended)"
        Y1[Single VM<br/>docker-compose]
        Y2[Cost: ~$5/month]
        Y3[Provisioning: 30-60s]
    end
    
    subgraph "Railway (Alternative)"
        R1[3 Separate Services]
        R2[Cost: ~$15/month]
        R3[Provisioning: 60-90s]
    end
    
    Y1 --> Y2
    Y2 --> Y3
    
    R1 --> R2
    R2 --> R3
    
    style Y1 fill:#e8f5e8
    style Y2 fill:#c8e6c9
    style Y3 fill:#a5d6a7
    style R1 fill:#ffebee
    style R2 fill:#ffcdd2
    style R3 fill:#ef9a9a
```

## Request Tracing Example

```mermaid
sequenceDiagram
    participant Client
    participant Proxy as gRPC Proxy
    participant VM1 as Tenant VM 1
    participant VM2 as Tenant VM 2
    
    rect rgb(200, 230, 200)
        Note over Client,VM1: Request 1 (tenant_123)
        Client->>Proxy: API: tenant_123-key<br/>RequestID: abc-123
        Proxy->>Proxy: Auth: tenant_123 ✅
        Proxy->>Proxy: Rate Limit: OK
        Proxy->>VM1: Route to 10.0.1.5:50051
        VM1-->>Proxy: Response
        Proxy-->>Client: Response (requestID: abc-123)
    end
    
    rect rgb(230, 200, 200)
        Note over Client,VM2: Request 2 (tenant_456)
        Client->>Proxy: API: tenant_456-key<br/>RequestID: def-456
        Proxy->>Proxy: Auth: tenant_456 ✅
        Proxy->>Proxy: Rate Limit: OK
        Proxy->>VM2: Route to 10.0.2.10:50051
        VM2-->>Proxy: Response
        Proxy-->>Client: Response (requestID: def-456)
    end
```

## Monitoring & Logging

```mermaid
graph TB
    subgraph "Request Lifecycle"
        Incoming[Incoming Request<br/>+ request_id]
        Auth[Authentication<br/>tenant_id, status]
        Routing[Routing<br/>target IP]
        Processing[Processing<br/>latency_ms]
        Response[Response<br/>status_code]
    end
    
    subgraph "Log Storage"
        Structured[Structured Logs<br/>JSON format]
        RequestDB[(request_logs<br/>table)]
        Alerts[Alerts<br/>disk space, errors]
    end
    
    Incoming --> Structured
    Auth --> Structured
    Routing --> Structured
    Processing --> Structured
    Response --> Structured
    
    Structured --> RequestDB
    Structured --> Alerts
    
    style Incoming fill:#e1f5ff
    style Auth fill:#fff9e1
    style Routing fill:#e8f5e8
    style Processing fill:#f3e5f5
    style Response fill:#ffe1e1
    style Structured fill:#fce4ec
```

## Security Architecture

```mermaid
flowchart TD
    Internet([Internet])
    
    subgraph "Security Layers"
        Firewall[Firewall<br/>Allowed Ports: 8080, 50051]
        SSLTLS[SSL/TLS<br/>Encryption in Transit]
        APIAuth[API Key<br/>Authentication]
        TenantIsolation[Tenant Isolation<br/>Separate VMs]
        RateLimit[Rate Limiting<br/>Per-tenant]
        NetworkSeg[Network Segmentation<br/>Internal Docker Network]
    end
    
    Internet --> Firewall
    Firewall --> SSLTLS
    SSLTLS --> APIAuth
    APIAuth --> TenantIsolation
    TenantIsolation --> RateLimit
    RateLimit --> NetworkSeg
    NetworkSeg --> TenantVM[Tenant VM Services]
    
    style Internet fill:#f3e5f5
    style Firewall fill:#ffe1e1
    style SSLTLS fill:#fff9e1
    style APIAuth fill:#e8f5e8
    style TenantIsolation fill:#e1f5ff
    style RateLimit fill:#fce4ec
    style NetworkSeg fill:#f3e5f5
    style TenantVM fill:#e8f5e8
```

## Cost Comparison

```mermaid
graph TB
    subgraph "Single VM Approach (Current)"
        S1[1 VM per tenant]
        S2[RoomService + MongoDB + Redis]
        S3[Cost: ~$5/month]
        S4[67% savings]
    end
    
    subgraph "Separate Services (Old)"
        M1[3 Services per tenant]
        M2[RoomService + MongoDB + Redis<br/>separately]
        M3[Cost: ~$15/month]
        M4[Higher complexity]
    end
    
    S1 --> S2
    S2 --> S3
    S3 --> S4
    
    M1 --> M2
    M2 --> M3
    M3 --> M4
    
    style S1 fill:#e8f5e8
    style S2 fill:#c8e6c9
    style S3 fill:#a5d6a7
    style S4 fill:#81c784
    style M1 fill:#ffebee
    style M2 fill:#ffcdd2
    style M3 fill:#ef9a9a
    style M4 fill:#ef5350
```

## Horizontal Scaling

```mermaid
graph TB
    LB[Load Balancer]
    
    subgraph "Proxy Cluster"
        Proxy1[Proxy Instance 1<br/>:50051]
        Proxy2[Proxy Instance 2<br/>:50051]
        Proxy3[Proxy Instance 3<br/>:50051]
    end
    
    subgraph "Shared Database"
        PG[(PostgreSQL<br/>Primary)]
        Replica[(PostgreSQL<br/>Replica)]
    end
    
    LB --> Proxy1
    LB --> Proxy2
    LB --> Proxy3
    
    Proxy1 --> PG
    Proxy2 --> PG
    Proxy3 --> PG
    
    PG --> Replica
    
    style LB fill:#f3e5f5
    style Proxy1 fill:#e1f5ff
    style Proxy2 fill:#e1f5ff
    style Proxy3 fill:#e1f5ff
    style PG fill:#fff9e1
    style Replica fill:#fff9e1
```

## Key Features Summary

| Feature | Description |
|---------|-------------|
| **Multi-tenancy** | Each tenant gets isolated VM with docker-compose |
| **Containerized Services** | RoomService, MongoDB, Redis all run as containers |
| **gRPC Proxying** | Intelligent routing based on API keys |
| **Connection Pooling** | Reuse connections for better performance |
| **Request Tracing** | Unique request ID for end-to-end tracking |
| **Authentication** | API key validation per tenant |
| **Rate Limiting** | Per-tenant request limits |
| **Auto-provisioning** | Automated VM deployment with docker-compose |
| **Health Monitoring** | Continuous health checks for all containers |
| **Cost Optimization** | 67% savings with single VM approach |
| **Logging** | Comprehensive request logging with tenant context |

## Future Enhancements

1. **Auto-scaling**: Automatically add proxy instances under load
2. **Geo-distribution**: Deploy proxies in multiple regions  
3. **Advanced monitoring**: Prometheus, Grafana integration
4. **Backup automation**: Automated tenant VM backups
5. **Migration tools**: Easy tenant export/import
6. **API versioning**: Support multiple RoomService versions
7. **Webhook notifications**: Tenant status changes, alerts
8. **Custom domains**: Tenant-specific subdomains
