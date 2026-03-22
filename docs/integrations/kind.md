### 3. `kind.md` (Kubernetes IN Docker for Local Testing)
# Harbor Satellite — kind Integration

`kind` (Kubernetes IN Docker) is ideal for locally testing Harbor Satellite. Because kind nodes run entirely inside Docker containers, we configure the registry mirror at the time of cluster creation.

> **Note:** This guide assumes Harbor and Ground Control are reachable from your host machine's Docker network.

---

## Step 1 — Create kind Cluster with Mirror Config

Instead of using a DaemonSet to alter node configurations post-deployment, we instruct `kind` to configure `containerd` to mirror `docker.io` to `localhost:5000` during cluster creation.

Create your cluster using this configuration file:

```bash
cat > kind-cluster.yaml << 'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["http://localhost:5000", "https://registry-1.docker.io"]
  [plugins."io.containerd.grpc.v1.cri".registry.configs."localhost:5000".tls]
    insecure_skip_verify = true
EOF

kind create cluster --config kind-cluster.yaml
Step 2 — Register Satellite
Run these commands on your Ground Control server:

Bash
# 1. Login
AUTH_TOKEN=$(curl -s -X POST http://localhost:8080/login -d '{"username":"admin","password":"Harbor12345"}' | jq -r '.token')

# 2. Register Satellite
SAT_TOKEN=$(curl -s -X POST http://localhost:8080/api/satellites \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"name":"kind-satellite","config_name":"default"}' | jq -r '.token')

# 3. Get image digest and assign group
DIGEST=$(curl -s -u "admin:Harbor12345" -H "Accept: application/vnd.docker.distribution.manifest.v2+json" "http://localhost:8090/v2/library/nginx/manifests/alpine" -D - -o /dev/null | grep -i docker-content-digest | awk '{print $2}' | tr -d '\r')

curl -s -X POST http://localhost:8080/api/groups/sync \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{\"group\":\"k8s-images\",\"registry\":\"http://<YOUR_HARBOR_IP>:8090\",\"artifacts\":[{\"repository\":\"library/nginx\",\"tag\":[\"alpine\"],\"type\":\"image\",\"digest\":\"${DIGEST}\"}]}"

curl -s -X POST http://localhost:8080/api/groups/satellite \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"satellite":"kind-satellite","group":"k8s-images"}'

Step 3 — Deploy Satellite DaemonSet
Because kind nodes are isolated Docker containers, hostNetwork: true binds to the kind node's network, effectively making localhost:5000 resolvable by containerd inside the node.

Bash
GC_IP="<GROUND_CONTROL_IP>"
HARBOR_IP="<HARBOR_IP>"

cat > satellite-kind.yaml << EOF
apiVersion: v1
kind: Namespace
metadata:
  name: harbor-satellite
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: satellite-config
  namespace: harbor-satellite
data:
  GROUND_CONTROL_URL: "http://${GC_IP}:8080"
  TOKEN: "${SAT_TOKEN}"
  HARBOR_REGISTRY_URL: "http://${HARBOR_IP}:8090"
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: harbor-satellite
  namespace: harbor-satellite
spec:
  selector:
    matchLabels:
      app: harbor-satellite
  template:
    metadata:
      labels:
        app: harbor-satellite
    spec:
      hostNetwork: true
      tolerations:
        - operator: Exists
      containers:
        - name: satellite
          image: registry.goharbor.io/harbor-satellite/satellite:latest
          envFrom:
            - configMapRef:
                name: satellite-config
          ports:
            - containerPort: 5000
              hostPort: 5000
          volumeMounts:
            - name: satellite-data
              mountPath: /var/lib/satellite
      volumes:
        - name: satellite-data
          hostPath:
            path: /var/lib/satellite
            type: DirectoryOrCreate
EOF

kubectl apply -f satellite-kind.yaml
kubectl rollout status daemonset/harbor-satellite -n harbor-satellite
Step 4 — Verify
Bash
sleep 30
kubectl run test-nginx --image=nginx:alpine --restart=Never
kubectl wait pod test-nginx --for=condition=Ready --timeout=60s
kubectl describe pod test-nginx | grep "Events" -A 10
kubectl delete pod test-nginx