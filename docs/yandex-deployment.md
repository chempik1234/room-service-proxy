# Yandex Cloud Deployment Guide

This guide explains how to deploy the room-service-proxy to Yandex Cloud using compute instances.

## Architecture

**🌐 Yandex Compute Instances + Binary Deployment**

- **Service**: Host-run binary (not containerized)
- **Database/Cache**: Docker containers on each instance
- **Networking**: SSH-based deployment and management
- **Advantages**: Better resource control, simpler than Docker orchestration

## Prerequisites

### 1. Yandex Cloud Account
- Active Yandex Cloud account
- Service account with compute instance permissions
- SSH key pair for instance access

### 2. Install Yandex CLI (yc)
```bash
# Install yc CLI
curl https://storage.yandexcloud.net/yc/install.sh | bash

# Initialize
yc init
```

### 3. Configure Environment
```bash
cp .env.yandex.example .env
# Edit .env with your Yandex Cloud credentials
```

## Deployment Options

### Option 1: GitHub Actions (Recommended)

**🚀 Automated deployment on commit**

1. **Setup GitHub Secrets**:
   ```
   YANDEK_HOST=your-instance-ip
   YANDEK_USER=yandex
   YANDEK_SSH_KEY=your-ssh-private-key
   ```

2. **Push to trigger deployment**:
   ```bash
   git add .
   git commit -m "Deploy to Yandex Cloud"
   git push origin master
   ```

3. **Workflow** (`.github/deploy.yml`):
   - SSH into Yandex instance
   - Stop existing service
   - Download latest binary
   - Restart service
   - Health check

### Option 2: Manual Deployment

**🔧 Direct SSH deployment**

```bash
# Connect to your Yandex instance
ssh -i ~/.ssh/yandex_key yandex@<instance-ip>

# Deploy manually
mkdir -p /opt/roomservice
cd /opt/roomservice

# Download binary
wget https://github.com/chempik1234/room-service-proxy/releases/latest/download/roomservice-proxy-linux-amd64.tar.gz

# Extract and install
tar -xzf roomservice-proxy-linux-amd64.tar.gz
sudo cp roomservice-proxy /usr/local/bin/

# Create systemd service
sudo cat > /etc/systemd/system/roomservice-proxy.service << 'EOF'
[Unit]
Description=RoomService Proxy
After=network.target

[Service]
Type=simple
User=yandex
WorkingDirectory=/opt/roomservice
EnvironmentFile=/opt/roomservice/.env
ExecStart=/usr/local/bin/roomservice-proxy
Restart=always

[Install]
WantedBy=multi-user.target
EOF

# Enable and start service
sudo systemctl enable roomservice-proxy
sudo systemctl start roomservice-proxy
```

## Multi-Tenant Architecture

### **How It Works**

Each tenant gets isolated compute instances:

```
┌─────────────────────────────────────────────────────┐
│                  Yandex Cloud                        │
├─────────────────────────────────────────────────────┤
│  Tenant A (tenant-xyz)                               │
│  ├── Instance: tenant-xyz (2 vCPU, 2GB RAM)         │
│  ├── Docker: MongoDB (port 27017)                   │
│  ├── Docker: Redis (port 6379)                       │
│  └── Binary: RoomService (port 50051)                │
├─────────────────────────────────────────────────────┤
│  Tenant B (tenant-abc)                               │
│  ├── Instance: tenant-abc (2 vCPU, 2GB RAM)         │
│  ├── Docker: MongoDB (port 27017)                   │
│  ├── Docker: Redis (port 6379)                       │
│  └── Binary: RoomService (port 50051)                │
└─────────────────────────────────────────────────────┘
```

### **Resource Management**

**Per-Tenant Resources:**
- **2 vCPU, 2GB RAM** per tenant
- **20GB disk** for databases
- **Separate compute instances** for isolation

**Cost Estimation:**
- ~₽500-1000/month per tenant (standard-v2 platform)
- Pay only for running instances
- Can stop instances when not in use

## Configuration

### **Environment Variables**

```bash
# .env file
DEPLOYMENT_PROVIDER=yandex
YANDEX_FOLDER_ID=b1g1234567890abcdef
YANDEX_ZONE=ru-central1-a
YANDEX_SERVICE_ACCOUNT_KEY=/path/to/key.json
YANDEX_SSH_KEY_PATH=~/.ssh/id_rsa
```

### **Service Configuration**

The proxy service reads configuration from:
1. Environment variables
2. Configuration file (`/opt/roomservice/.env`)
3. Database (tenant-specific settings)

## Tenant Provisioning

### **Creating a New Tenant**

```bash
# Create tenant via API
curl -X POST http://your-proxy:8080/api/tenants \
  -H "Authorization: Bearer <admin-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "acme-corp",
    "email": "admin@acme.com",
    "plan": "pro"
  }'
```

### **What Happens**

1. **Tenant record created** in PostgreSQL
2. **Yandex instances provisioned**:
   - `acme-corp-mongo` (MongoDB instance)
   - `acme-corp-redis` (Redis instance)
   - `acme-corp` (RoomService instance)
3. **Docker containers deployed** via SSH:
   - MongoDB container on mongo instance
   - Redis container on redis instance
   - RoomService binary on app instance
4. **Health checks** verify all services are running
5. **Tenant marked as active** with connection details

## Advantages Over Railway

### **✅ Yandex Cloud Benefits**

1. **Better Performance**:
   - More CPU/RAM per tenant
   - Faster than Railway's shared containers

2. **Cost Control**:
   - Pay per instance
   - Stop instances when not needed
   - No daily service limits

3. **Isolation**:
   - Separate compute instances per tenant
   - Better security and performance isolation

4. **Simplicity**:
   - Binary deployment (no Docker orchestration)
   - SSH-based management
   - Standard systemd services

### **⚠️ Trade-offs**

1. **More Manual Setup**:
   - Need to manage SSH keys
   - Configure firewall rules
   - Monitor instance health

2. **Higher Base Cost**:
   - Minimum cost per instance
   - Railway may be cheaper for small workloads

## Monitoring and Management

### **Check Instance Status**

```bash
# List all instances for your folder
yc compute instance list --folder-id b1g1234567890abcdef

# Get instance details
yc compute instance get tenant-xyz --folder-id b1g1234567890abcdef

# Check instance logs
yc compute instance logs --follow tenant-xyz --folder-id b1g1234567890abcdef
```

### **SSH Access**

```bash
# SSH to specific instance
ssh -i ~/.ssh/yandex_key yandex@<instance-ip>

# Check service status
sudo systemctl status roomservice-proxy

# View logs
sudo journalctl -u roomservice-proxy -f
```

### **Scaling**

```bash
# Stop tenant instance (save costs when not in use)
yc compute instance stop tenant-xyz --folder-id b1g1234567890abcdef

# Start tenant instance
yc compute instance start tenant-xyz --folder-id b1g1234567890abcdef

# Delete tenant instance (cleanup)
yc compute instance delete --name tenant-xyz --folder-id b1g1234567890abcdef
```

## Troubleshooting

### **Common Issues**

#### **SSH Connection Failed**
```bash
# Check SSH key permissions
chmod 400 ~/.ssh/yandex_key

# Verify instance is running
yc compute instance list --folder-id b1g1234567890abcdef
```

#### **Instance Not Responding**
```bash
# Check instance status
yc compute instance get <instance-name> --folder-id b1g1234567890abcdef

# Restart instance
yc compute instance restart <instance-name> --folder-id b1g1234567890abcdef
```

#### **Service Won't Start**
```bash
# Check service logs
sudo journalctl -u roomservice-proxy -n 50

# Verify environment variables
sudo systemctl show-environment roomservice-proxy

# Manual start
sudo /usr/local/bin/roomservice-proxy --config /opt/roomservice/.env
```

## Next Steps

1. **Test with Railway first** to verify the unified architecture works
2. **Configure Yandex credentials** in `.env` file
3. **Set up GitHub Actions** for automated deployment
4. **Create first tenant** to test Yandex provisioning
5. **Monitor costs** and optimize instance usage

## Support

For issues specific to:
- **Yandex Cloud**: Check Yandex Cloud documentation or `yc --help`
- **Proxy Service**: Check room-service-proxy GitHub issues
- **Deployment**: See deployment logs and systemd journals

---

**Ready to deploy?** Start with Railway to verify everything works, then switch to Yandex for production! 🚀