#!/bin/bash
# master-demo.sh
# Runs the full Harbor Satellite quickstart demo end-to-end.
# All variables are defined upfront. Run pre-demo-var-check.sh first.
set -euo pipefail

# ═══════════════════════════════════════════════════════════════════════════════
# CONFIGURATION - Edit these before running
# ═══════════════════════════════════════════════════════════════════════════════

CLOUD_IP="10.147.106.55"
HARBOR_URL="http://${CLOUD_IP}:8080"
HARBOR_USERNAME="admin"
HARBOR_PASSWORD="Harbor12345"
ADMIN_PASSWORD="Harbor12345"

SAT_USER="sat-1"
SAT_IP="10.147.106.144"
SAT_PASS="password"
SAT_UID="1000"
SAT_NAME="us-east-1"

DEMO_IMAGE="library/nginx"
DEMO_TAG="latest"
GROUP_NAME="edge-images"

GC_HOST_PORT="9080"
SPIRE_HOST_PORT="9081"

WORK_DIR="$HOME/quickstart"

# ═══════════════════════════════════════════════════════════════════════════════
# HELPERS
# ═══════════════════════════════════════════════════════════════════════════════

BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
RESET="\033[0m"

step()    { echo -e "\n${BOLD}${GREEN}===> $1${RESET}"; }
info()    { echo -e "     ${CYAN}$1${RESET}"; }
waiting() { echo -e "     ${YELLOW}$1${RESET}"; }

# SSH wrapper (uses key-based auth — run ssh-copy-id first)
remote() {
    ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
        "${SAT_USER}@${SAT_IP}" "$@"
}

remote_bg() {
    # Run a command on the remote host in the background.
    # Redirect stdin, stdout, stderr and disown so SSH can exit cleanly.
    ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
        "${SAT_USER}@${SAT_IP}" "nohup $1 </dev/null > $2 2>&1 & disown; sleep 1"
}

scp_to() {
    scp -o StrictHostKeyChecking=no "$@"
}

wait_for_healthy() {
    local cmd="$1"
    local label="$2"
    local max_attempts="${3:-30}"
    local attempt=1
    waiting "Waiting for $label to become healthy..."
    while [ $attempt -le "$max_attempts" ]; do
        if eval "$cmd" &>/dev/null; then
            info "$label is healthy."
            return 0
        fi
        sleep 3
        attempt=$((attempt + 1))
    done
    echo "ERROR: $label did not become healthy after $max_attempts attempts"
    exit 1
}

# ═══════════════════════════════════════════════════════════════════════════════
# PRE-FLIGHT
# ═══════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║           Harbor Satellite Demo - master-demo.sh            ║"
echo "╠═══════════════════════════════════════════════════════════════╣"
echo "║  Cloud Server  : $CLOUD_IP                          ║"
echo "║  Harbor         : $HARBOR_URL               ║"
echo "║  Edge Device    : ${SAT_USER}@${SAT_IP}                  ║"
echo "║  Satellite Name : $SAT_NAME                            ║"
echo "║  Demo Image     : $DEMO_IMAGE:$DEMO_TAG                ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${RESET}"

# Verify SSH connectivity
if ! ssh -o ConnectTimeout=5 -o BatchMode=yes "${SAT_USER}@${SAT_IP}" "true" &>/dev/null; then
    echo "ERROR: Cannot SSH to ${SAT_USER}@${SAT_IP} without password."
    echo "Run: ssh-copy-id ${SAT_USER}@${SAT_IP}"
    exit 1
fi

echo "Press Enter to start the demo, or Ctrl+C to abort..."
read -r

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 0: DEPLOY APPS ON K3S (THEY WILL FAIL — NO MIRROR YET)
# ═══════════════════════════════════════════════════════════════════════════════

step "Step 0.1: Configuring k3s mirror (satellite not running yet)"
info "Setting up k3s to try satellite (localhost:5000) first."
info "Since the satellite isn't running, all image pulls will FAIL."

remote bash -s << K3S_EOF
sudo mkdir -p /etc/rancher/k3s
sudo tee /etc/rancher/k3s/registries.yaml > /dev/null << 'REGEOF'
mirrors:
  "${CLOUD_IP}:8080":
    endpoint:
      - "http://127.0.0.1:5000"
      - "http://${CLOUD_IP}:8080"
REGEOF

echo "registries.yaml written:"
sudo cat /etc/rancher/k3s/registries.yaml

echo "Restarting k3s to pick up mirror config..."
sudo systemctl restart k3s
K3S_EOF

waiting "Waiting for k3s to come back up..."
sleep 10

ATTEMPTS=0
MAX_K3S_ATTEMPTS=20
while [ $ATTEMPTS -lt $MAX_K3S_ATTEMPTS ]; do
    if remote "sudo k3s kubectl get nodes" 2>/dev/null | grep -qi "ready"; then
        info "k3s is back up and ready."
        break
    fi
    ATTEMPTS=$((ATTEMPTS + 1))
    sleep 3
done

# ─── 0.2 Deploy nginx ────────────────────────────────────────────────────────
step "Step 0.2: Deploying nginx on k3s (will fail — no images available)"

remote bash -s << NGINX_EOF
sudo k3s kubectl apply -f - << 'YAMLEOF'
apiVersion: v1
kind: Namespace
metadata:
  name: nginx
---
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: nginx
  labels:
    app: nginx
spec:
  containers:
  - name: nginx
    image: ${CLOUD_IP}:8080/library/nginx:latest
    ports:
    - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: nginx
  namespace: nginx
spec:
  type: NodePort
  ports:
  - port: 80
    targetPort: 80
    nodePort: 31080
  selector:
    app: nginx
YAMLEOF
NGINX_EOF

# ─── 0.3 Deploy Example Voting App ───────────────────────────────────────────
step "Step 0.3: Deploying Example Voting App on k3s (will fail — no images available)"

remote bash -s << VOTE_EOF
sudo k3s kubectl apply -f - << 'YAMLEOF'
apiVersion: v1
kind: Namespace
metadata:
  name: voting-app
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: voting-app
  labels:
    app: redis
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
      - name: redis
        image: ${CLOUD_IP}:8080/library/redis:alpine
        ports:
        - containerPort: 6379
        volumeMounts:
        - mountPath: /data
          name: redis-data
      volumes:
      - name: redis-data
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: voting-app
spec:
  type: ClusterIP
  ports:
  - port: 6379
    targetPort: 6379
  selector:
    app: redis
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: db
  namespace: voting-app
  labels:
    app: db
spec:
  replicas: 1
  selector:
    matchLabels:
      app: db
  template:
    metadata:
      labels:
        app: db
    spec:
      containers:
      - name: postgres
        image: ${CLOUD_IP}:8080/library/postgres:15-alpine
        env:
        - name: POSTGRES_USER
          value: postgres
        - name: POSTGRES_PASSWORD
          value: postgres
        ports:
        - containerPort: 5432
        volumeMounts:
        - mountPath: /var/lib/postgresql/data
          name: db-data
      volumes:
      - name: db-data
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: db
  namespace: voting-app
spec:
  type: ClusterIP
  ports:
  - port: 5432
    targetPort: 5432
  selector:
    app: db
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: worker
  namespace: voting-app
  labels:
    app: worker
spec:
  replicas: 1
  selector:
    matchLabels:
      app: worker
  template:
    metadata:
      labels:
        app: worker
    spec:
      containers:
      - name: worker
        image: ${CLOUD_IP}:8080/library/examplevotingapp_worker:latest
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vote
  namespace: voting-app
  labels:
    app: vote
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vote
  template:
    metadata:
      labels:
        app: vote
    spec:
      containers:
      - name: vote
        image: ${CLOUD_IP}:8080/library/examplevotingapp_vote:latest
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: vote
  namespace: voting-app
spec:
  type: NodePort
  ports:
  - port: 8080
    targetPort: 80
    nodePort: 31000
  selector:
    app: vote
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: result
  namespace: voting-app
  labels:
    app: result
spec:
  replicas: 1
  selector:
    matchLabels:
      app: result
  template:
    metadata:
      labels:
        app: result
    spec:
      containers:
      - name: result
        image: ${CLOUD_IP}:8080/library/examplevotingapp_result:latest
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: result
  namespace: voting-app
spec:
  type: NodePort
  ports:
  - port: 8081
    targetPort: 80
    nodePort: 31001
  selector:
    app: result
YAMLEOF
VOTE_EOF

sleep 10
info "Current pod status (expected: ImagePullBackOff):"
remote "sudo k3s kubectl get pods -A --no-headers 2>/dev/null | grep -E 'nginx|voting-app'" 2>/dev/null || true

echo ""
echo -e "${BOLD}${YELLOW}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║  All pods are failing — no satellite mirror running yet!    ║"
echo "║  Now we'll set up Harbor Satellite to fix this.             ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${RESET}"
echo "Press Enter to begin satellite setup..."
read -r

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 1: CLOUD SIDE
# ═══════════════════════════════════════════════════════════════════════════════

# ─── 1.1 Directory structure ─────────────────────────────────────────────────
step "Step 1.1: Creating directory structure"
info "Creating $WORK_DIR/gc/spire and $WORK_DIR/sat"

mkdir -p "$WORK_DIR/gc/spire" "$WORK_DIR/sat"
cd "$WORK_DIR/gc"

info "Working directory: $(pwd)"

# ─── 1.2 Generate certificates ──────────────────────────────────────────────
step "Step 1.2: Generating X.509 certificates"
info "Creating SPIRE upstream CA, x509pop CA, and agent certificates."
info "The satellite cert CN=$SAT_NAME must match the name used during registration."

mkdir -p certs

# SPIRE upstream authority CA
openssl genrsa -out certs/ca.key 4096 2>/dev/null
openssl req -new -x509 -days 365 -key certs/ca.key -out certs/ca.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=SPIRE CA" 2>/dev/null

# X.509 PoP CA
openssl genrsa -out certs/x509pop-ca.key 4096 2>/dev/null
openssl req -new -x509 -days 365 -key certs/x509pop-ca.key -out certs/x509pop-ca.crt \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=X509 PoP CA" 2>/dev/null

# Ground Control agent certificate
openssl genrsa -out certs/agent-gc.key 2048 2>/dev/null
openssl req -new -key certs/agent-gc.key -out certs/agent-gc.csr \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=agent-gc" 2>/dev/null
cat > certs/agent-gc.ext << 'EXTEOF'
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names
[alt_names]
URI.1 = spiffe://harbor-satellite.local/agent/ground-control
EXTEOF
openssl x509 -req -days 365 -in certs/agent-gc.csr \
    -CA certs/x509pop-ca.crt -CAkey certs/x509pop-ca.key -CAcreateserial \
    -out certs/agent-gc.crt -extfile certs/agent-gc.ext 2>/dev/null

# Satellite agent certificate
openssl genrsa -out certs/${SAT_NAME}.key 2048 2>/dev/null
openssl req -new -key certs/${SAT_NAME}.key -out certs/${SAT_NAME}.csr \
    -subj "/C=US/ST=State/L=City/O=Harbor Satellite/CN=${SAT_NAME}" 2>/dev/null
cat > certs/${SAT_NAME}.ext << EXTEOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names
[alt_names]
URI.1 = spiffe://harbor-satellite.local/agent/satellite
EXTEOF
openssl x509 -req -days 365 -in certs/${SAT_NAME}.csr \
    -CA certs/x509pop-ca.crt -CAkey certs/x509pop-ca.key -CAcreateserial \
    -out certs/${SAT_NAME}.crt -extfile certs/${SAT_NAME}.ext 2>/dev/null

rm -f certs/*.csr certs/*.ext certs/*.srl
chmod 644 certs/*.key certs/*.crt

info "Certificates generated:"
ls -1 certs/

# ─── 1.3 SPIRE Server config ────────────────────────────────────────────────
step "Step 1.3: Creating SPIRE Server config"
info "Trust domain: harbor-satellite.local"
info "NodeAttestor: x509pop (certificate-based, no tokens)"

cat > spire/server.conf << 'EOF'
server {
    bind_address = "0.0.0.0"
    bind_port = "8081"
    socket_path = "/tmp/spire-server/private/api.sock"
    trust_domain = "harbor-satellite.local"
    data_dir = "/opt/spire/data/server"
    log_level = "INFO"
    ca_ttl = "24h"
    default_x509_svid_ttl = "1h"
    default_jwt_svid_ttl = "5m"
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "sqlite3"
            connection_string = "/opt/spire/data/server/datastore.sqlite3"
        }
    }
    NodeAttestor "x509pop" {
        plugin_data {
            ca_bundle_path = "/opt/spire/conf/server/x509pop-ca.crt"
        }
    }
    KeyManager "disk" {
        plugin_data {
            keys_path = "/opt/spire/data/server/keys.json"
        }
    }
    UpstreamAuthority "disk" {
        plugin_data {
            key_file_path = "/opt/spire/conf/server/ca.key"
            cert_file_path = "/opt/spire/conf/server/ca.crt"
        }
    }
}

health_checks {
    listener_enabled = true
    bind_address = "0.0.0.0"
    bind_port = "8080"
    live_path = "/live"
    ready_path = "/ready"
}
EOF

info "Created spire/server.conf"

# ─── 1.4 SPIRE Agent config for Ground Control ──────────────────────────────
step "Step 1.4: Creating SPIRE Agent config for Ground Control"
info "This agent runs alongside Ground Control inside Docker."
info "It attests using its x509pop certificate (no tokens needed)."

cat > spire/agent-gc.conf << 'EOF'
agent {
    data_dir = "/opt/spire/data/agent"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    trust_bundle_path = "/opt/spire/conf/agent/bootstrap.crt"
    trust_domain = "harbor-satellite.local"
}

plugins {
    NodeAttestor "x509pop" {
        plugin_data {
            private_key_path = "/opt/spire/conf/agent/agent.key"
            certificate_path = "/opt/spire/conf/agent/agent.crt"
        }
    }
    KeyManager "disk" {
        plugin_data {
            directory = "/opt/spire/data/agent"
        }
    }
    WorkloadAttestor "unix" {
        plugin_data {}
    }
    WorkloadAttestor "docker" {
        plugin_data {
            docker_socket_path = "unix:///var/run/docker.sock"
        }
    }
}

health_checks {
    listener_enabled = true
    bind_address = "0.0.0.0"
    bind_port = "8080"
    live_path = "/live"
    ready_path = "/ready"
}
EOF

info "Created spire/agent-gc.conf"

# ─── 1.5 Docker Compose file ────────────────────────────────────────────────
step "Step 1.5: Creating Docker Compose file"
info "Services: postgres, spire-server, spire-agent-gc, ground-control"
info "Harbor URL inside containers: ${HARBOR_URL}"

cat > docker-compose.yml << EOF
services:
  postgres:
    image: postgres:15-alpine
    container_name: harbor-satellite-postgres
    environment:
      POSTGRES_USER: harbor
      POSTGRES_PASSWORD: harbor
      POSTGRES_DB: harbor_satellite
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "harbor", "-d", "harbor_satellite"]
      interval: 5s
      timeout: 5s
      retries: 5
      start_period: 10s
    networks:
      - harbor-satellite

  spire-server:
    image: ghcr.io/spiffe/spire-server:1.14.1
    container_name: spire-server
    hostname: spire-server
    command: ["-config", "/opt/spire/conf/server/server.conf"]
    volumes:
      - ./spire/server.conf:/opt/spire/conf/server/server.conf:ro
      - ./certs/ca.crt:/opt/spire/conf/server/ca.crt:ro
      - ./certs/ca.key:/opt/spire/conf/server/ca.key:ro
      - ./certs/x509pop-ca.crt:/opt/spire/conf/server/x509pop-ca.crt:ro
      - spire-server-data:/opt/spire/data/server
      - spire-server-socket:/tmp/spire-server/private
    ports:
      - "${SPIRE_HOST_PORT}:8081"
    healthcheck:
      test: ["CMD", "/opt/spire/bin/spire-server", "healthcheck", "-socketPath", "/tmp/spire-server/private/api.sock"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    networks:
      - harbor-satellite

  spire-agent-gc:
    image: ghcr.io/spiffe/spire-agent:1.14.1
    container_name: spire-agent-gc
    hostname: spire-agent-gc
    pid: host
    command: ["-config", "/opt/spire/conf/agent/agent.conf"]
    volumes:
      - ./spire/agent-gc.conf:/opt/spire/conf/agent/agent.conf:ro
      - ./certs/ca.crt:/opt/spire/conf/agent/bootstrap.crt:ro
      - ./certs/agent-gc.crt:/opt/spire/conf/agent/agent.crt:ro
      - ./certs/agent-gc.key:/opt/spire/conf/agent/agent.key:ro
      - spire-agent-gc-data:/opt/spire/data/agent
      - spire-agent-gc-socket:/run/spire/sockets
      - /var/run/docker.sock:/var/run/docker.sock:ro
    depends_on:
      spire-server:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "/opt/spire/bin/spire-agent", "healthcheck", "-socketPath", "/run/spire/sockets/agent.sock"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    networks:
      - harbor-satellite

  ground-control:
    image: registry.goharbor.io/harbor-satellite/ground-control:latest
    container_name: ground-control
    environment:
      - DB_HOST=postgres
      - DB_PORT=5432
      - DB_DATABASE=harbor_satellite
      - DB_USERNAME=harbor
      - DB_PASSWORD=harbor
      - PORT=8080
      - APP_ENV=development
      - HARBOR_URL=${HARBOR_URL}
      - HARBOR_USERNAME=${HARBOR_USERNAME}
      - HARBOR_PASSWORD=${HARBOR_PASSWORD}
      - SKIP_HARBOR_HEALTH_CHECK=false
      - ADMIN_PASSWORD=${ADMIN_PASSWORD}
      - SPIFFE_ENABLED=true
      - SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock
      - SPIFFE_TRUST_DOMAIN=harbor-satellite.local
      - SPIRE_SERVER_SOCKET=/tmp/spire-server/private/api.sock
      - SPIRE_SERVER_ADDRESS=spire-server
      - SPIRE_SERVER_PORT=8081
      - SPIRE_TRUST_DOMAIN=harbor-satellite.local
    volumes:
      - spire-agent-gc-socket:/run/spire/sockets:ro
      - spire-server-socket:/tmp/spire-server/private:ro
    ports:
      - "${GC_HOST_PORT}:8080"
    depends_on:
      postgres:
        condition: service_healthy
      spire-agent-gc:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-sfk", "https://localhost:8080/ping"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 15s
    networks:
      - harbor-satellite

volumes:
  postgres-data:
  spire-server-data:
  spire-server-socket:
  spire-agent-gc-data:
  spire-agent-gc-socket:

networks:
  harbor-satellite:
    name: harbor-satellite
EOF

info "Created docker-compose.yml"

# ─── 1.6 Start PostgreSQL and SPIRE Server ───────────────────────────────────
step "Step 1.6: Starting PostgreSQL and SPIRE Server"
info "Pre-creating SPIRE data volume with correct permissions (non-root user fix)."

docker volume create gc_spire-server-data >/dev/null 2>&1 || true
docker run --rm -v gc_spire-server-data:/data alpine chmod 777 /data

info "Starting postgres and spire-server..."
docker compose up -d postgres spire-server

wait_for_healthy \
    "docker exec spire-server /opt/spire/bin/spire-server healthcheck -socketPath /tmp/spire-server/private/api.sock" \
    "SPIRE Server"

# ─── 1.7 Start SPIRE Agent and register Ground Control ──────────────────────
step "Step 1.7: Starting SPIRE Agent for Ground Control"
info "The GC agent attests using its x509pop certificate automatically."

docker compose up -d spire-agent-gc

wait_for_healthy \
    "docker exec spire-agent-gc /opt/spire/bin/spire-agent healthcheck -socketPath /run/spire/sockets/agent.sock" \
    "SPIRE Agent (GC)"

info "Discovering GC agent SPIFFE ID..."
sleep 3
GC_AGENT_ID=$(docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock \
    | grep "SPIFFE ID" | grep "x509pop" | head -1 | awk '{print $NF}')
info "GC Agent ID: $GC_AGENT_ID"

info "Registering Ground Control as a workload under this agent..."
docker exec spire-server /opt/spire/bin/spire-server entry create \
    -parentID "$GC_AGENT_ID" \
    -spiffeID spiffe://harbor-satellite.local/ground-control \
    -selector docker:label:com.docker.compose.service:ground-control \
    -socketPath /tmp/spire-server/private/api.sock

# ─── 1.8 Start Ground Control ───────────────────────────────────────────────
step "Step 1.8: Starting Ground Control"
info "Ground Control connects to Harbor at $HARBOR_URL"
info "It will be available at https://localhost:${GC_HOST_PORT}"

docker compose up -d ground-control

wait_for_healthy \
    "curl -sfk https://localhost:${GC_HOST_PORT}/ping" \
    "Ground Control"

info "Ground Control is up!"

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 2: EDGE SIDE (Satellite SPIRE Agent)
# ═══════════════════════════════════════════════════════════════════════════════

step "Step 2.1: Installing SPIRE agent on edge device (arm64)"
info "Downloading SPIRE v1.14.1 arm64 binary to ${SAT_USER}@${SAT_IP}"

remote "
    if command -v spire-agent >/dev/null 2>&1; then
        echo 'spire-agent already installed, skipping download'
    else
        curl -Lo /tmp/spire.tar.gz \
            https://github.com/spiffe/spire/releases/download/v1.14.1/spire-1.14.1-linux-arm64-musl.tar.gz
        cd /tmp && tar xzf spire.tar.gz
        sudo cp spire-1.14.1/bin/spire-agent /usr/local/bin/
        rm -rf /tmp/spire.tar.gz /tmp/spire-1.14.1
        echo 'spire-agent installed'
    fi
"

# ─── 2.2 Copy certificates to edge ──────────────────────────────────────────
step "Step 2.2: Copying certificates to edge device"
info "Transferring ca.crt, ${SAT_NAME}.crt, ${SAT_NAME}.key to the Pi"

remote "mkdir -p ~/quickstart/sat/certs"
scp_to \
    "certs/ca.crt" \
    "certs/${SAT_NAME}.crt" \
    "certs/${SAT_NAME}.key" \
    "${SAT_USER}@${SAT_IP}:quickstart/sat/certs/"

info "Certificates copied."

# ─── 2.3 Create SPIRE agent config on edge ───────────────────────────────────
step "Step 2.3: Creating SPIRE agent config on edge device"
info "server_address = ${CLOUD_IP}:${SPIRE_HOST_PORT}"
info "Attestation: x509pop with CN=${SAT_NAME}"

remote "cat > ~/quickstart/sat/${SAT_NAME}.conf << 'AGENTEOF'
agent {
    data_dir = \"./data/agent\"
    log_level = \"INFO\"
    server_address = \"${CLOUD_IP}\"
    server_port = \"${SPIRE_HOST_PORT}\"
    socket_path = \"/tmp/spire-agent/agent.sock\"
    trust_bundle_path = \"./certs/ca.crt\"
    trust_domain = \"harbor-satellite.local\"
}

plugins {
    NodeAttestor \"x509pop\" {
        plugin_data {
            private_key_path = \"./certs/${SAT_NAME}.key\"
            certificate_path = \"./certs/${SAT_NAME}.crt\"
        }
    }
    KeyManager \"disk\" {
        plugin_data {
            directory = \"./data/agent\"
        }
    }
    WorkloadAttestor \"unix\" {
        plugin_data {}
    }
}

health_checks {
    listener_enabled = true
    bind_address = \"0.0.0.0\"
    bind_port = \"8080\"
    live_path = \"/live\"
    ready_path = \"/ready\"
}
AGENTEOF"

info "Config created on edge device."

# ─── 2.4 Start SPIRE agent on edge ──────────────────────────────────────────
step "Step 2.4: Starting SPIRE agent on edge device"
info "Cleaning up any previous agent state and starting fresh."

# Cleanup first (separate call)
remote "pkill spire-agent 2>/dev/null; sleep 1; rm -rf ~/quickstart/sat/data/agent; mkdir -p ~/quickstart/sat/data/agent; rm -f /tmp/spire-agent/agent.sock; echo done"

# Start agent with full stdin/stdout/stderr detach using -f flag on ssh
ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -f \
    "${SAT_USER}@${SAT_IP}" \
    "cd ~/quickstart/sat && spire-agent run -config ${SAT_NAME}.conf > /tmp/spire-agent.log 2>&1"

echo "SPIRE agent starting..."

waiting "Waiting for satellite SPIRE agent to attest with server..."
sleep 5

ATTEMPTS=0
MAX_ATTEMPTS=20
while [ $ATTEMPTS -lt $MAX_ATTEMPTS ]; do
    if remote "spire-agent healthcheck -socketPath /tmp/spire-agent/agent.sock 2>&1" 2>/dev/null | grep -qi "healthy"; then
        info "Satellite SPIRE agent is healthy and attested!"
        break
    fi
    ATTEMPTS=$((ATTEMPTS + 1))
    sleep 3
done

if [ $ATTEMPTS -eq $MAX_ATTEMPTS ]; then
    echo "ERROR: Satellite SPIRE agent did not become healthy."
    echo "Check logs on the Pi: cat /tmp/spire-agent.log"
    exit 1
fi

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 3: REGISTER SATELLITE AND CREATE GROUPS (Cloud side)
# ═══════════════════════════════════════════════════════════════════════════════

step "Step 3.1: Logging into Ground Control"
info "Authenticating as admin to get JWT token."

LOGIN_RESP=$(curl -sk -X POST "https://localhost:${GC_HOST_PORT}/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD}\"}")
AUTH_TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

if [ -z "$AUTH_TOKEN" ]; then
    echo "ERROR: Failed to get auth token from Ground Control"
    echo "Response: $LOGIN_RESP"
    exit 1
fi
info "Got auth token: ${AUTH_TOKEN:0:20}..."

# ─── 3.2 Register satellite ─────────────────────────────────────────────────
step "Step 3.2: Registering satellite '${SAT_NAME}'"
info "GC will match the attested agent by CN=${SAT_NAME} (x509pop)."
info "This creates: satellite record + SPIRE workload entry + Harbor robot account."
info "Selector: unix:uid:${SAT_UID} (matches the satellite process owner on the Pi)."

REG_RESP=$(curl -sk -X POST "https://localhost:${GC_HOST_PORT}/api/satellites/register" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{
      \"satellite_name\": \"${SAT_NAME}\",
      \"region\": \"${SAT_NAME}\",
      \"selectors\": [\"unix:uid:${SAT_UID}\"],
      \"attestation_method\": \"x509pop\"
    }")

echo "$REG_RESP" | jq .

if echo "$REG_RESP" | jq -e '.message' 2>/dev/null | grep -qi "fail\|error"; then
    echo "ERROR: Satellite registration failed"
    exit 1
fi

info "Satellite registered successfully."

# ─── 3.3 Create group with image ────────────────────────────────────────────
step "Step 3.3: Creating group '${GROUP_NAME}' with image ${DEMO_IMAGE}:${DEMO_TAG}"
info "Fetching image digest from Harbor..."

DIGEST=$(curl -sk -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
    "${HARBOR_URL}/api/v2.0/projects/library/repositories/nginx/artifacts?q=tags%3D${DEMO_TAG}&page_size=1" \
    | jq -r '.[0].digest // empty')

if [ -z "$DIGEST" ]; then
    echo "ERROR: Could not fetch digest for ${DEMO_IMAGE}:${DEMO_TAG} from Harbor"
    exit 1
fi
info "Image digest: ${DIGEST}"

info "Creating group and syncing artifacts..."
GROUP_RESP=$(curl -sk -X POST "https://localhost:${GC_HOST_PORT}/api/groups/sync" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{
      \"group\": \"${GROUP_NAME}\",
      \"registry\": \"${HARBOR_URL}\",
      \"artifacts\": [
        {
          \"repository\": \"${DEMO_IMAGE}\",
          \"tag\": [\"${DEMO_TAG}\"],
          \"type\": \"image\",
          \"digest\": \"${DIGEST}\"
        }
      ]
    }")

echo "$GROUP_RESP" | jq . 2>/dev/null || echo "$GROUP_RESP"
info "Group created."

# ─── 3.4 Assign group to satellite ──────────────────────────────────────────
step "Step 3.4: Assigning group '${GROUP_NAME}' to satellite '${SAT_NAME}'"
info "After this, Ground Control knows ${SAT_NAME} should replicate ${DEMO_IMAGE}:${DEMO_TAG}."

ASSIGN_RESP=$(curl -sk -X POST "https://localhost:${GC_HOST_PORT}/api/groups/satellite" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{\"satellite\": \"${SAT_NAME}\", \"group\": \"${GROUP_NAME}\"}")

echo "$ASSIGN_RESP" | jq . 2>/dev/null || echo "$ASSIGN_RESP"
info "Group assigned to satellite."

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 4: START THE SATELLITE (Edge side)
# ═══════════════════════════════════════════════════════════════════════════════

step "Step 4.1: Copying satellite binary to edge device"
info "Copying pre-built arm64 binary from ~/harbor-satellite-arm64"

SAT_BINARY="$HOME/harbor-satellite-arm64"
if [ ! -f "$SAT_BINARY" ]; then
    echo "ERROR: Satellite binary not found at $SAT_BINARY"
    echo "Build it first: GOOS=linux GOARCH=arm64 go build -o ~/harbor-satellite-arm64 cmd/main.go"
    exit 1
fi

scp_to "$SAT_BINARY" "${SAT_USER}@${SAT_IP}:~/quickstart/sat/harbor-satellite"
remote "chmod +x ~/quickstart/sat/harbor-satellite"
info "Binary copied and ready."

step "Step 4.2: Starting satellite on edge device"
info "The satellite connects to Ground Control at https://${CLOUD_IP}:${GC_HOST_PORT}"
info "It uses SPIFFE for zero-trust registration (no tokens needed at runtime)."
info "Upstream registry: ${HARBOR_URL}"
info "Local Zot registry: http://0.0.0.0:5000 (HTTP, using --use-unsecure)"

# Use ssh -f to fork into background (same fix as SPIRE agent)
ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -f \
    "${SAT_USER}@${SAT_IP}" \
    "cd ~/quickstart/sat && ./harbor-satellite --ground-control-url https://${CLOUD_IP}:${GC_HOST_PORT} --spiffe-enabled --spiffe-endpoint-socket unix:///tmp/spire-agent/agent.sock --harbor-registry-url ${HARBOR_URL} --use-unsecure > /tmp/satellite.log 2>&1"

info "Satellite started in background. Logs at /tmp/satellite.log on the Pi."

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 5: VERIFY
# ═══════════════════════════════════════════════════════════════════════════════

step "Step 5: Verifying the setup"
info "Waiting for satellite to replicate the image (this may take 30-60 seconds)..."
sleep 15

# Check satellite status in GC
info "Satellite status in Ground Control:"
curl -sk "https://localhost:${GC_HOST_PORT}/api/satellites" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" | jq .

# Check SPIRE agents
info "SPIRE agents (should see 2 - GC and satellite):"
docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock 2>/dev/null | grep "SPIFFE ID" || true

# Check satellite local registry
info "Checking satellite local registry at ${SAT_IP}:5000..."
sleep 15

CATALOG=$(remote "curl -s http://127.0.0.1:5000/v2/_catalog" 2>/dev/null || echo "")
if echo "$CATALOG" | grep -q "${DEMO_IMAGE}"; then
    info "Image ${DEMO_IMAGE} is available in satellite's local registry!"
else
    waiting "Image not yet available. It may still be replicating."
    waiting "Check logs: ssh ${SAT_USER}@${SAT_IP} 'tail -50 /tmp/satellite.log'"
fi

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 6: VERIFY PODS RECOVERING (nginx should start pulling now)
# ═══════════════════════════════════════════════════════════════════════════════

step "Step 6: Restarting failed pods (satellite mirror is now available)"
info "The satellite is running on port 5000. Restarting pods so k3s pulls fresh."
info "Recreating nginx pod and rolling out voting-app deployments..."

remote bash -s << RESTART_EOF
# Recreate nginx pod (static pod, must delete + create)
sudo k3s kubectl delete pod nginx -n nginx --force --grace-period=0 2>/dev/null || true
sleep 2
sudo k3s kubectl run nginx --image=${CLOUD_IP}:8080/library/nginx:latest \
    -n nginx --restart=Never --labels=app=nginx 2>/dev/null || true

# Rollout restart voting-app deployments to trigger fresh pulls
for dep in redis db worker vote result; do
    sudo k3s kubectl rollout restart deployment/\$dep -n voting-app 2>/dev/null || true
done
echo "All pods restarted."
RESTART_EOF

waiting "Waiting for nginx pod to come up..."
ATTEMPTS=0
while [ $ATTEMPTS -lt 15 ]; do
    POD_STATUS=$(remote "sudo k3s kubectl get pod nginx -n nginx -o jsonpath='{.status.phase}'" 2>/dev/null || echo "unknown")
    if [ "$POD_STATUS" = "Running" ]; then
        info "nginx pod is Running! Image pulled through satellite mirror."
        break
    fi
    ATTEMPTS=$((ATTEMPTS + 1))
    sleep 3
done

if [ $ATTEMPTS -eq 15 ]; then
    waiting "nginx pod status: $POD_STATUS. Check: ssh ${SAT_USER}@${SAT_IP} 'sudo k3s kubectl describe pod nginx -n nginx'"
fi

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 7: ADD VOTING APP IMAGES TO GROUP
# ═══════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}${CYAN}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║  Next: Add Docker Example Voting App images to the group    ║"
echo "║                                                             ║"
echo "║  Images to add:                                             ║"
echo "║    - library/redis:alpine                                   ║"
echo "║    - library/postgres:15-alpine                             ║"
echo "║    - library/examplevotingapp_worker:latest                 ║"
echo "║    - library/examplevotingapp_result:latest                 ║"
echo "║    - library/examplevotingapp_vote:latest                   ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${RESET}"
echo "Press Enter to add voting app images, or Ctrl+C to stop here..."
read -r

step "Step 7: Adding Docker Example Voting App images to group '${GROUP_NAME}'"

# image:tag pairs (redis and postgres use specific tags, not latest)
VOTING_ENTRIES=(
    "library/redis:alpine"
    "library/postgres:15-alpine"
    "library/examplevotingapp_worker:latest"
    "library/examplevotingapp_result:latest"
    "library/examplevotingapp_vote:latest"
)

ARTIFACTS_JSON="["
FIRST=true

for ENTRY in "${VOTING_ENTRIES[@]}"; do
    IMG="${ENTRY%%:*}"
    TAG="${ENTRY##*:}"
    REPO_NAME="${IMG#library/}"
    info "Fetching digest for ${IMG}:${TAG} ..."

    IMG_DIGEST=$(curl -sk -u "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" \
        "${HARBOR_URL}/api/v2.0/projects/library/repositories/${REPO_NAME}/artifacts?q=tags%3D${TAG}&page_size=1" \
        | jq -r '.[0].digest // empty')

    if [ -z "$IMG_DIGEST" ]; then
        echo "WARNING: Could not fetch digest for ${IMG}:${TAG} — skipping"
        continue
    fi
    info "  ${IMG}:${TAG} -> ${IMG_DIGEST:0:24}..."

    if [ "$FIRST" = true ]; then
        FIRST=false
    else
        ARTIFACTS_JSON+=","
    fi

    ARTIFACTS_JSON+="{\"repository\":\"${IMG}\",\"tag\":[\"${TAG}\"],\"type\":\"image\",\"digest\":\"${IMG_DIGEST}\"}"
done

ARTIFACTS_JSON+="]"

info "Syncing voting app images to group '${GROUP_NAME}'..."
SYNC_RESP=$(curl -sk -X POST "https://localhost:${GC_HOST_PORT}/api/groups/sync" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{
      \"group\": \"${GROUP_NAME}\",
      \"registry\": \"${HARBOR_URL}\",
      \"artifacts\": ${ARTIFACTS_JSON}
    }")

echo "$SYNC_RESP" | jq . 2>/dev/null || echo "$SYNC_RESP"
info "Voting app images added to group."

info "Waiting for satellite to replicate voting app images (this may take 60+ seconds)..."
sleep 30

info "Checking satellite local registry catalog..."
CATALOG=$(remote "curl -s http://127.0.0.1:5000/v2/_catalog" 2>/dev/null || echo "")
echo "     $CATALOG" | jq . 2>/dev/null || echo "     $CATALOG"

REPLICATED=0
for ENTRY in "${VOTING_ENTRIES[@]}"; do
    IMG="${ENTRY%%:*}"
    if echo "$CATALOG" | grep -q "${IMG}"; then
        info "  ✓ ${IMG} replicated"
        REPLICATED=$((REPLICATED + 1))
    else
        waiting "  … ${IMG} not yet available (may still be replicating)"
    fi
done
info "${REPLICATED}/${#VOTING_ENTRIES[@]} voting app images replicated so far."

# ═══════════════════════════════════════════════════════════════════════════════
# STEP 8: VERIFY ALL PODS RECOVERED
# ═══════════════════════════════════════════════════════════════════════════════

step "Step 8: Verifying all pods have recovered"
info "Satellite is running with all images. k3s should now pull from localhost:5000."
info "Pods that were in ImagePullBackOff should start recovering..."

waiting "Waiting for voting app pods to recover..."
ATTEMPTS=0
MAX_ATTEMPTS=30
ALL_RUNNING=false
while [ $ATTEMPTS -lt $MAX_ATTEMPTS ]; do
    NGINX_STATUS=$(remote "sudo k3s kubectl get pod nginx -n nginx -o jsonpath='{.status.phase}'" 2>/dev/null || echo "unknown")
    VOTING_READY=$(remote "sudo k3s kubectl get pods -n voting-app --no-headers 2>/dev/null | grep -c Running || echo 0" 2>/dev/null | tr -d '[:space:]')
    VOTING_READY="${VOTING_READY:-0}"

    if [ "$NGINX_STATUS" = "Running" ] && [ "$VOTING_READY" -ge 5 ]; then
        ALL_RUNNING=true
        break
    fi
    info "  nginx: $NGINX_STATUS | voting-app: ${VOTING_READY}/5 running"
    ATTEMPTS=$((ATTEMPTS + 1))
    sleep 5
done

echo ""
info "Final pod status:"
remote "sudo k3s kubectl get pods -A --no-headers 2>/dev/null | grep -E 'nginx|voting-app'" 2>/dev/null || true

echo ""
if [ "$ALL_RUNNING" = true ]; then
    echo -e "${BOLD}${GREEN}All pods recovered and running! Images pulled through satellite mirror.${RESET}"
else
    echo -e "${BOLD}${YELLOW}Some pods may still be recovering. Check: ssh ${SAT_USER}@${SAT_IP} 'sudo k3s kubectl get pods -A'${RESET}"
fi

info "Services:"
remote "sudo k3s kubectl get svc -n voting-app" 2>/dev/null || true
remote "sudo k3s kubectl get svc -n nginx" 2>/dev/null || true

echo ""
echo -e "${BOLD}${GREEN}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║                    Demo Complete!                            ║"
echo "╠═══════════════════════════════════════════════════════════════╣"
echo "║                                                             ║"
echo "║  Ground Control : https://localhost:${GC_HOST_PORT}               ║"
echo "║  Satellite Reg  : http://${SAT_IP}:5000             ║"
echo "║  k3s mirror     : ${CLOUD_IP}:8080 -> localhost:5000     ║"
echo "║                                                             ║"
echo "║  Apps deployed on k3s:                                      ║"
echo "║    nginx        : http://${SAT_IP}:31080             ║"
echo "║    vote UI      : http://${SAT_IP}:31000             ║"
echo "║    result UI    : http://${SAT_IP}:31001             ║"
echo "║                                                             ║"
echo "║  Demo story:                                                ║"
echo "║    1. Apps deployed → ImagePullBackOff (no mirror)          ║"
echo "║    2. Satellite set up → images replicated to edge          ║"
echo "║    3. Pods recovered → pulling from local satellite         ║"
echo "║                                                             ║"
echo "║  Useful commands:                                           ║"
echo "║    Satellite logs : ssh ${SAT_USER}@${SAT_IP} tail -f /tmp/satellite.log  ║"
echo "║    Local catalog  : curl http://${SAT_IP}:5000/v2/_catalog  ║"
echo "║    All pods       : ssh ${SAT_USER}@${SAT_IP} sudo k3s kubectl get pods -A  ║"
echo "║                                                             ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${RESET}"
