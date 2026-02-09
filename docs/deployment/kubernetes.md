# Kubernetes Deployment

Deploy Harbor Satellite on Kubernetes clusters. This guide covers basic setup using example manifests.

**Important:** These are example manifests to demonstrate deployment concepts, not a supported installation method. For production deployments, adapt these examples to your specific requirements and operational practices.

## Overview

Kubernetes deployment includes:

- **Ground Control** — Deployment in cloud cluster  
- **PostgreSQL** — Database (managed service or StatefulSet)
- **Satellite** — Deployment on edge cluster nodes

## Prerequisites

- Kubernetes 1.20+ clusters
- `kubectl` configured
- Harbor with satellite support
- Network connectivity between clusters

## Part 1: Ground Control (Cloud Cluster)

### Create Namespace and Secret

```bash
kubectl create namespace ground-control

kubectl create secret generic ground-control-secrets \
  --from-literal=db-password=your-password \
  --from-literal=harbor-token=your-robot-token \
  -n ground-control
```

### Deploy PostgreSQL

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
  namespace: ground-control
spec:
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: postgres:15
        env:
        - name: POSTGRES_DB
          value: satellite
        - name: POSTGRES_USER
          value: satellite
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: db-password
        ports:
        - containerPort: 5432
        volumeMounts:
        - name: postgres-data
          mountPath: /var/lib/postgresql/data
      volumes:
      - name: postgres-data
        emptyDir: {}  # Use PVC for production
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: ground-control
spec:
  selector:
    app: postgres
  ports:
  - port: 5432
```

### Deploy Ground Control

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ground-control
  namespace: ground-control
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ground-control
  template:
    metadata:
      labels:
        app: ground-control
    spec:
      containers:
      - name: ground-control
        image: harbor-satellite/ground-control:latest
        ports:
        - containerPort: 8080
        env:
        - name: DB_HOST
          value: postgres
        - name: DB_DATABASE
          value: satellite
        - name: DB_USERNAME
          value: satellite
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: db-password
        - name: HARBOR_TOKEN
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: harbor-token
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: ground-control
  namespace: ground-control
spec:
  type: LoadBalancer
  ports:
  - port: 8080
    targetPort: 8080
  selector:
    app: ground-control
```

## Part 2: Satellite (Edge Cluster)

### Create Configuration

```bash
kubectl create namespace satellite

cat > satellite-config.yaml << 'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: satellite-config
  namespace: satellite
data:
  config.yaml: |
    satellite:
      ground_control:
        url: http://your-ground-control:8080
      registry:
        enabled: true
        url: localhost:5000
      auth:
        token: your-robot-token
EOF

kubectl apply -f satellite-config.yaml
```

### Deploy Satellite

**Note:** The example below assumes a containerd-based runtime and privileged access for registry integration. Adjust volume mounts and security context based on your cluster runtime and security policies.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: satellite
  namespace: satellite
spec:
  replicas: 1
  selector:
    matchLabels:
      app: satellite
  template:
    metadata:
      labels:
        app: satellite
    spec:
      hostNetwork: true
      containers:
      - name: satellite
        image: harbor-satellite/satellite:latest
        ports:
        - containerPort: 5000
        volumeMounts:
        - name: config
          mountPath: /opt/satellite/config
        - name: container-runtime
          mountPath: /var/run/containerd
          readOnly: true
        securityContext:
          privileged: true  # Required for container runtime integration
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
          limits:
            memory: "1Gi"
            cpu: "1"
      volumes:
      - name: config
        configMap:
          name: satellite-config
      - name: container-runtime
        hostPath:
          path: /var/run/containerd  # Adjust for your container runtime
          type: Socket
---
apiVersion: v1
kind: Service
metadata:
  name: satellite
  namespace: satellite
spec:
  type: NodePort
  ports:
  - port: 5000
    targetPort: 5000
    nodePort: 30500
  selector:
    app: satellite
```

## Verification

```bash
# Check Ground Control
kubectl get pods -n ground-control
kubectl logs deployment/ground-control -n ground-control

# Check Satellite
kubectl get pods -n satellite
kubectl logs deployment/satellite -n satellite

# Port forward to test
kubectl port-forward svc/ground-control 8080:8080 -n ground-control
kubectl port-forward svc/satellite 5000:5000 -n satellite
```

## Container Runtime Configuration

After satellite is running, configure worker nodes to use the satellite registry. The examples below assume containerd - adjust for your specific container runtime.

### For containerd

```bash
# On each worker node
mkdir -p /etc/containerd/certs.d/docker.io
cat > /etc/containerd/certs.d/docker.io/hosts.toml << 'EOF'
server = "https://docker.io"

[host."http://satellite:5000"]
  capabilities = ["pull", "resolve"]
EOF

systemctl restart containerd
```

## Security Considerations

- Satellite requires privileged mode for container runtime integration
- Use proper RBAC and network policies in production
- Configure TLS and proper authentication based on your security requirements

## Production Considerations

For production deployments:

- Use managed database services instead of in-cluster PostgreSQL
- Implement proper persistent storage with PVCs
- Add resource quotas and limits appropriate for your environment
- Configure monitoring, logging, and alerting per your operational practices
- Follow your organization's security and compliance requirements
- Consider using Helm charts or operators for easier management

## Multiple Edge Clusters

To deploy satellites across multiple edge clusters:

1. Repeat the satellite deployment on each edge cluster
2. Use unique configuration for each satellite location
3. Organize satellites into groups via Ground Control
4. Consider using GitOps tools for consistent configuration management

## Troubleshooting

```bash
# Check pod status
kubectl describe pod <pod-name> -n <namespace>

# View logs
kubectl logs <pod-name> -n <namespace> -f

# Test connectivity
kubectl exec -it <pod-name> -n <namespace> -- curl http://service:port/health
```

## Next Steps

- See [Docker Deployment](docker.md) for single-node setups
- Review [Configuration Reference](../configuration.md) for options
- Check [Troubleshooting Guide](../troubleshooting.md) for issues
- Adapt these examples to your specific operational requirements
