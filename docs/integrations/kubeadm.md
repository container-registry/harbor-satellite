# Harbor Satellite — kubeadm Integration

Standard Kubernetes clusters created with kubeadm use `containerd` as the default container runtime. This guide covers deploying Harbor Satellite as a DaemonSet to automatically mirror images at the edge.

> **Note:** This guide uses **Token-based registration** for simplicity. For Zero-Trust identity, refer to the [SPIFFE/SPIRE Quickstart](../quickstart.md).

## Step 1 — Register Satellite

Run these commands on your Ground Control server to register the cluster and assign an image group.

```bash
# 1. Login to Ground Control
LOGIN_RESP=$(curl -s -X POST http://localhost:8080/login \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"Harbor12345"}')
AUTH_TOKEN=$(echo "$LOGIN_RESP" | jq -r '.token')

# 2. Register the satellite
SAT_RESP=$(curl -s -X POST http://localhost:8080/api/satellites \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"name":"kubeadm-satellite","config_name":"default"}')
SAT_TOKEN=$(echo "$SAT_RESP" | jq -r '.token')
echo "Your Satellite Token: $SAT_TOKEN"

# 3. Get target image digest (e.g., nginx:alpine)
DIGEST=$(curl -s -u "admin:Harbor12345" \
    -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
    "http://localhost:8090/v2/library/nginx/manifests/alpine" \
    -D - -o /dev/null | grep -i docker-content-digest | awk '{print $2}' | tr -d '\r')

# 4. Create and assign image group
curl -s -X POST http://localhost:8080/api/groups/sync \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d "{\"group\":\"k8s-images\",\"registry\":\"http://<YOUR_HARBOR_IP>:8090\",\"artifacts\":[{\"repository\":\"library/nginx\",\"tag\":[\"alpine\"],\"type\":\"image\",\"digest\":\"${DIGEST}\"}]}"

curl -s -X POST http://localhost:8080/api/groups/satellite \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{"satellite":"kubeadm-satellite","group":"k8s-images"}'
```

## Step 2 — Deploy Satellite DaemonSet

Deploy the Satellite agents to the cluster. Update `GC_IP`, `HARBOR_IP`, and `SAT_TOKEN` before applying.

```bash
GC_IP="<GROUND_CONTROL_IP>"
HARBOR_IP="<HARBOR_IP>"
SAT_TOKEN="<YOUR_TOKEN>"

cat > satellite-kubeadm.yaml << EOF
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

kubectl apply -f satellite-kubeadm.yaml
kubectl rollout status daemonset/harbor-satellite -n harbor-satellite
```

## Step 3 — Configure containerd Mirror (Automated)

This DaemonSet runs a privileged Init Container to write the mirror configuration (`hosts.toml`) to every node and seamlessly restarts containerd.

```bash
cat > mirror-config.yaml << 'EOF'
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: containerd-mirror-config
  namespace: harbor-satellite
spec:
  selector:
    matchLabels:
      app: containerd-mirror-config
  template:
    metadata:
      labels:
        app: containerd-mirror-config
    spec:
      hostPID: true
      tolerations:
        - operator: Exists
      initContainers:
        - name: write-mirror-config
          image: alpine:latest
          securityContext:
            privileged: true
          command:
            - sh
            - -c
            - |
              mkdir -p /host/etc/containerd/certs.d/docker.io
              cat > /host/etc/containerd/certs.d/docker.io/hosts.toml << 'TOML'
              server = "[https://registry-1.docker.io](https://registry-1.docker.io)"
              [host."http://localhost:5000"]
                capabilities = ["pull", "resolve"]
                skip_verify = true
              TOML
              nsenter -t 1 -m -u -i -n -- systemctl restart containerd
          volumeMounts:
            - name: host-etc
              mountPath: /host/etc
      containers:
        - name: pause
          image: gcr.io/google-containers/pause:3.9
      volumes:
        - name: host-etc
          hostPath:
            path: /etc
EOF

kubectl apply -f mirror-config.yaml
```

## Step 4 — Verify

```bash
# 1. Wait for image sync
sleep 30

# 2. Deploy test workload
kubectl run test-nginx --image=nginx:alpine --restart=Never
kubectl wait pod test-nginx --for=condition=Ready --timeout=60s

# 3. Check events to confirm local pull
kubectl describe pod test-nginx | grep "Events" -A 10
kubectl delete pod test-nginx
```