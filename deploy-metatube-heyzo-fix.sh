#!/bin/bash
#
# MetaTube HEYZO Provider Fix - Docker Compose Deploy Script
#
# Usage:
#   chmod +x deploy-metatube-heyzo-fix.sh
#   ./deploy-metatube-heyzo-fix.sh
#

set -e

# Configuration
IMAGE_NAME="metatube-server"
IMAGE_TAG="dev-fix"
FORK_REPO="https://github.com/jacobli97/metatube-sdk-go.git"
CLONE_DIR="/tmp/metatube-sdk-go-fix"
DEPLOY_DIR="/opt/metatube"

echo "========================================"
echo "MetaTube HEYZO Provider Fix - Deploy"
echo "========================================"
echo ""

# Step 1: Clone the forked repository
echo "[1/4] Cloning forked repository..."
if [ -d "$CLONE_DIR" ]; then
    rm -rf "$CLONE_DIR"
fi
git clone --depth 1 "$FORK_REPO" "$CLONE_DIR"
echo "Done."
echo ""

# Step 2: Build Docker image
echo "[2/4] Building Docker image (this may take a few minutes)..."
cd "$CLONE_DIR"
docker build -t "$IMAGE_NAME:$IMAGE_TAG" .
echo "Done."
echo ""

# Step 3: Deploy docker-compose.yml
echo "[3/4] Setting up docker compose..."
mkdir -p "$DEPLOY_DIR"
cp "$CLONE_DIR/docker-compose.yml" "$DEPLOY_DIR/docker-compose.yml"
echo "Compose file saved to $DEPLOY_DIR/docker-compose.yml"
echo ""

# Step 4: Stop old container and start with docker compose
echo "[4/4] Starting with docker compose..."
# Stop old standalone container if exists
docker stop metatube-dev 2>/dev/null || true
docker rm metatube-dev 2>/dev/null || true

cd "$DEPLOY_DIR"
docker compose up -d

echo ""
echo "========================================"
echo "Deployment complete!"
echo "========================================"
echo ""
echo "Container is running on port 9090."
echo ""
echo "Test:"
echo "  curl http://localhost:9090/v1/movies/search?q=HEYZO-3977&provider=HEYZO"
echo ""
echo "View logs:"
echo "  docker compose -f $DEPLOY_DIR/docker-compose.yml logs -f"
echo ""
echo "Stop:"
echo "  docker compose -f $DEPLOY_DIR/docker-compose.yml down"
echo ""
echo "Restart:"
echo "  docker compose -f $DEPLOY_DIR/docker-compose.yml restart"
echo ""
