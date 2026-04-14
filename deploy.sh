#!/bin/bash
# RoomService Proxy - Railway Deployment Script

echo "🚀 Deploying RoomService Proxy to Railway..."

# Check if Railway CLI is installed
if ! command -v railway &> /dev/null; then
    echo "❌ Railway CLI not found. Installing..."
    npm install -g @railway/cli
fi

# Login to Railway
echo "📝 Logging into Railway..."
railway login

# Create new project
echo "📦 Creating Railway project..."
railway new --name roomservice-proxy

# Add PostgreSQL
echo "🗄️  Adding PostgreSQL service..."
railway add postgresql

# Wait for PostgreSQL to be ready
echo "⏳ Waiting for PostgreSQL to be ready..."
sleep 10

# Get DATABASE_URL
echo "🔗 Getting DATABASE_URL..."
DATABASE_URL=$(railway variables get RAILWAY_POSTGRESERVICE_URL | head -n 1)
echo "DATABASE_URL: $DATABASE_URL"

# Set environment variables
echo "⚙️  Setting environment variables..."
railway variables set DATABASE_URL="$DATABASE_URL"
railway variables set ADMIN_API_KEY="rs_admin_$(openssl rand -hex 16)"
railway variables set GRPC_PORT="50051"
railway variables set ADMIN_PORT="8080"
railway variables set RATE_LIMIT_RPS="100"
railway variables set RATE_LIMIT_WINDOW="60s"
railway variables set RATE_LIMIT_BURST="10"
railway variables set ENABLE_AUTH="true"
railway variables set ENABLE_RATE_LIMIT="true"

# Connect to database and run schema
echo "📊 Setting up database schema..."
echo "Copy this SQL and run it in the psql console that opens:"
echo "----------------------------------------"
cat schema.sql
echo "----------------------------------------"

railway connect

echo "✅ Environment configured!"
echo ""
echo "📝 Next steps:"
echo "1. In the psql console that opened, paste the schema SQL above"
echo "2. Type '\\q' to exit psql when done"
echo "3. Run: railway up"
echo "4. Your proxy will be live at: https://roomservice-proxy.up.railway.app"
