***

### 2. `k3s.md` (Lightweight Edge Kubernetes)

```markdown
# Harbor Satellite — K3s Integration

K3s is highly optimized for edge environments. Unlike kubeadm, K3s manages its containerd configuration centrally via `registries.yaml`. This guide covers deploying Harbor Satellite on K3s.

> **Note:** This guide uses **Token-based registration** for simplicity. For Zero-Trust identity, refer to the [SPIFFE/SPIRE Quickstart](../quickstart.md).

---
Step 1 & 2 — Register and Deploy Satellite
(Note: Follow Step 1 and Step 2 exactly as documented in the kubeadm.md guide. The API calls and the Harbor Satellite DaemonSet are identical across Kubernetes distributions.)

Step 3 — Configure K3s Registry Mirror (Automated)
K3s requires mirror configurations to be placed in /etc/rancher/k3s/registries.yaml. This DaemonSet automatically generates this file across all K3s server and agent nodes and restarts the K3s service.

Bash
cat > k3s-mirror-config.yaml << 'EOF'
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: k3s-mirror-config
  namespace: harbor-satellite
spec:
  selector:
    matchLabels:
      app: k3s-mirror-config
  template:
    metadata:
      labels:
        app: k3s-mirror-config
    spec:
      hostPID: true
      tolerations:
        - operator: Exists
      initContainers:
        - name: write-registries-yaml
          image: alpine:latest
          securityContext:
            privileged: true
          command:
            - sh
            - -c
            - |
              mkdir -p /host/etc/rancher/k3s
              cat > /host/etc/rancher/k3s/registries.yaml << 'YAML'
              mirrors:
                docker.io:
                  endpoint:
                    - "http://localhost:5000"
                    - "[https://registry-1.docker.io](https://registry-1.docker.io)"
              configs:
                "localhost:5000":
                  tls:
                    insecure_skip_verify: true
              YAML
              # Restart k3s-agent (or k3s server) to apply changes
              nsenter -t 1 -m -u -i -n -- systemctl try-restart k3s-agent || true
              nsenter -t 1 -m -u -i -n -- systemctl try-restart k3s || true
          volumeMounts:
            - name: host-k3s-etc
              mountPath: /host/etc/rancher/k3s
      containers:
        - name: pause
          image: rancher/pause:3.6
      volumes:
        - name: host-k3s-etc
          hostPath:
            path: /etc/rancher/k3s
EOF

kubectl apply -f k3s-mirror-config.yaml
Step 4 — Verify
Bash
# 1. Wait for image sync
sleep 30

# 2. Deploy test workload
kubectl run test-nginx --image=nginx:alpine --restart=Never
kubectl wait pod test-nginx --for=condition=Ready --timeout=60s

# 3. Check events to confirm local pull
kubectl describe pod test-nginx | grep "Events" -A 10
kubectl delete pod test-nginx