#!/bin/bash
set -e

# ── Configuration ──
VPS_IP="144.91.111.151"
VPS_USER="root"
REMOTE_DIR="/root/high_throughput_ledger"
REPO_URL="https://github.com/Dv1704/high_throughpu_ledger.git"

# ── Password Handling ──
if [ -z "$VPS_PASS" ]; then
    echo "🔐 Enter VPS password for $VPS_USER@$VPS_IP:"
    read -s VPS_PASS
fi
export SSHPASS=$VPS_PASS

echo "🚀 Deploying High-Throughput Ledger to $VPS_IP..."

# ── Remote Setup Script ──
SETUP_SCRIPT='
set -e
echo "📦 Installing dependencies..."

# Install Go if not present
if ! command -v go &> /dev/null; then
    echo "Installing Go 1.24..."
    wget -q https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
    rm go1.24.0.linux-amd64.tar.gz
    echo "export PATH=\$PATH:/usr/local/go/bin" >> /root/.bashrc
    export PATH=$PATH:/usr/local/go/bin
fi
export PATH=$PATH:/usr/local/go/bin

# Install Postgres if not present
if ! command -v psql &> /dev/null; then
    echo "Installing PostgreSQL..."
    apt-get update -qq
    apt-get install -y -qq postgresql postgresql-contrib
    systemctl enable postgresql
    systemctl start postgresql
fi

# Install Redis if not present
if ! command -v redis-server &> /dev/null; then
    echo "Installing Redis..."
    apt-get update -qq
    apt-get install -y -qq redis-server
    systemctl enable redis-server
    systemctl start redis-server
fi

# Clone or update repo
if [ -d "'$REMOTE_DIR'" ]; then
    cd '$REMOTE_DIR'
    git pull origin main || true
else
    git clone '$REPO_URL' '$REMOTE_DIR'
    cd '$REMOTE_DIR'
fi

# Setup databases
echo "🗄️ Setting up databases..."
sudo -u postgres psql -c "CREATE DATABASE ledger_shard_1;" 2>/dev/null || echo "DB ledger_shard_1 exists"
sudo -u postgres psql -c "CREATE DATABASE ledger_shard_2;" 2>/dev/null || echo "DB ledger_shard_2 exists"
sudo -u postgres psql -c "CREATE USER ledger WITH PASSWORD '\''ledger123'\'';" 2>/dev/null || echo "User exists"
sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE ledger_shard_1 TO ledger;" 2>/dev/null
sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE ledger_shard_2 TO ledger;" 2>/dev/null
sudo -u postgres psql -d ledger_shard_1 -c "GRANT ALL ON SCHEMA public TO ledger;" 2>/dev/null
sudo -u postgres psql -d ledger_shard_2 -c "GRANT ALL ON SCHEMA public TO ledger;" 2>/dev/null

PGPASSWORD=ledger123 psql -U ledger -d ledger_shard_1 -f init.sql 2>/dev/null || echo "Schema exists on shard 1"
PGPASSWORD=ledger123 psql -U ledger -d ledger_shard_2 -f init.sql 2>/dev/null || echo "Schema exists on shard 2"

# Build the application
echo "🔨 Building application..."
cd '$REMOTE_DIR'
go mod download
go build -o ledger-engine .

# Stop old process if running
pkill -f ledger-engine 2>/dev/null || true
sleep 1

# Start the application
echo "🚀 Starting application..."
export DB_SHARD_1_URL="postgres://ledger:ledger123@localhost:5432/ledger_shard_1?sslmode=disable"
export DB_SHARD_2_URL="postgres://ledger:ledger123@localhost:5432/ledger_shard_2?sslmode=disable"
export REDIS_URL="localhost:6379"

nohup ./ledger-engine > /var/log/ledger-engine.log 2>&1 &
sleep 3

# Verify
if curl -s http://localhost:8080/health | grep -q "healthy"; then
    echo "✅ Deployment successful! Server is healthy."
    echo "📖 Swagger UI: http://'$VPS_IP':8080/swagger"
    echo "📡 API: http://'$VPS_IP':8080"
else
    echo "❌ Health check failed. Check logs: /var/log/ledger-engine.log"
    tail -20 /var/log/ledger-engine.log
fi
'

# ── Execute Remote Deploy ──
sshpass -e ssh -o StrictHostKeyChecking=no $VPS_USER@$VPS_IP "$SETUP_SCRIPT"

echo ""
echo "════════════════════════════════════════════"
echo "  ✅ Deployment Complete!"
echo "  📖 Swagger: http://$VPS_IP:8080/swagger"
echo "  📡 API:     http://$VPS_IP:8080"
echo "  🏥 Health:  http://$VPS_IP:8080/health"
echo "════════════════════════════════════════════"
