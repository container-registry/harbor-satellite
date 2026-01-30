# Kubernetes Deployment

This guide covers deploying Harbor Satellite on Kubernetes using Helm charts and Kubernetes manifests.

## Prerequisites

- Kubernetes 1.24+
- Helm 3.0+
- kubectl configured
- Storage class for persistent volumes
- Ingress controller (nginx, traefik, etc.)

## Quick Start with Helm

### Add Harbor Satellite Helm Repository

```bash
helm repo add harbor-satellite https://harbor-satellite.github.io/helm-charts
helm repo update
```

### Install Ground Control

```bash
# Create namespace
kubectl create namespace harbor-satellite

# Install Ground Control
helm install ground-control harbor-satellite/ground-control \
  --namespace harbor-satellite \
  --set harbor.url="https://harbor.example.com" \
  --set harbor.username="robot\$account" \
  --set harbor.password="secure-token" \
  --set adminPassword="SecurePass123" \
  --set ingress.enabled=true \
  --set ingress.host="ground-control.example.com"
```

### Install Satellite

```bash
# Install Satellite
helm install satellite harbor-satellite/satellite \
  --namespace harbor-satellite \
  --set groundControl.url="https://ground-control.example.com" \
  --set satellite.token="your-satellite-token" \
  --set registry.storage.size="50Gi"
```

## Production Deployment

### Ground Control Deployment

Create a comprehensive Ground Control deployment:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: harbor-satellite

---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ground-control-config
  namespace: harbor-satellite
data:
  HARBOR_URL: "https://harbor.example.com"
  SESSION_DURATION_HOURS: "24"
  LOCKOUT_DURATION_MINUTES: "15"

---
apiVersion: v1
kind: Secret
metadata:
  name: ground-control-secrets
  namespace: harbor-satellite
type: Opaque
data:
  HARBOR_USERNAME: <base64-encoded-robot-username>
  HARBOR_PASSWORD: <base64-encoded-robot-password>
  ADMIN_PASSWORD: <base64-encoded-admin-password>
  DB_PASSWORD: <base64-encoded-db-password>

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ground-control
  namespace: harbor-satellite
spec:
  replicas: 2
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
        - containerPort: 9090
          name: http
        env:
        - name: HARBOR_URL
          valueFrom:
            configMapKeyRef:
              name: ground-control-config
              key: HARBOR_URL
        - name: HARBOR_USERNAME
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: HARBOR_USERNAME
        - name: HARBOR_PASSWORD
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: HARBOR_PASSWORD
        - name: ADMIN_PASSWORD
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: ADMIN_PASSWORD
        - name: DB_HOST
          value: "postgres"
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: DB_PASSWORD
        livenessProbe:
          httpGet:
            path: /health
            port: 9090
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 9090
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"

---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: harbor-satellite
spec:
  serviceName: postgres
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
        image: postgres:15-alpine
        ports:
        - containerPort: 5432
          name: postgres
        env:
        - name: POSTGRES_DB
          value: "groundcontrol"
        - name: POSTGRES_USER
          value: "groundcontrol"
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: ground-control-secrets
              key: DB_PASSWORD
        volumeMounts:
        - name: postgres-storage
          mountPath: /var/lib/postgresql/data
        livenessProbe:
          exec:
            command:
            - pg_isready
            - -U
            - groundcontrol
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          exec:
            command:
            - pg_isready
            - -U
            - groundcontrol
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "1Gi"
            cpu: "500m"
  volumeClaimTemplates:
  - metadata:
      name: postgres-storage
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi

---
apiVersion: v1
kind: Service
metadata:
  name: ground-control
  namespace: harbor-satellite
spec:
  selector:
    app: ground-control
  ports:
  - port: 9090
    targetPort: 9090
    name: http
  type: ClusterIP

---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ground-control
  namespace: harbor-satellite
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - ground-control.example.com
    secretName: ground-control-tls
  rules:
  - host: ground-control.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: ground-control
            port:
              number: 9090
```

### Satellite Deployment

Create a Satellite deployment:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: satellite-config
  namespace: harbor-satellite
data:
  GROUND_CONTROL_URL: "https://ground-control.example.com"
  LOG_LEVEL: "info"
  JSON_LOGGING: "true"

---
apiVersion: v1
kind: Secret
metadata:
  name: satellite-secrets
  namespace: harbor-satellite
type: Opaque
data:
  SATELLITE_TOKEN: <base64-encoded-satellite-token>

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: satellite
  namespace: harbor-satellite
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
      containers:
      - name: satellite
        image: harbor-satellite/satellite:latest
        ports:
        - containerPort: 8585
          name: registry
        env:
        - name: GROUND_CONTROL_URL
          valueFrom:
            configMapKeyRef:
              name: satellite-config
              key: GROUND_CONTROL_URL
        - name: SATELLITE_TOKEN
          valueFrom:
            secretKeyRef:
              name: satellite-secrets
              key: SATELLITE_TOKEN
        - name: LOG_LEVEL
          valueFrom:
            configMapKeyRef:
              name: satellite-config
              key: LOG_LEVEL
        volumeMounts:
        - name: zot-storage
          mountPath: /var/lib/zot
        livenessProbe:
          httpGet:
            path: /v2/
            port: 8585
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /v2/
            port: 8585
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
      volumes:
      - name: zot-storage
        persistentVolumeClaim:
          claimName: zot-pvc

---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: zot-pvc
  namespace: harbor-satellite
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 50Gi

---
apiVersion: v1
kind: Service
metadata:
  name: satellite-registry
  namespace: harbor-satellite
spec:
  selector:
    app: satellite
  ports:
  - port: 5000
    targetPort: 8585
    name: registry
  type: ClusterIP

---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: satellite-registry
  namespace: harbor-satellite
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - registry.example.com
    secretName: registry-tls
  rules:
  - host: registry.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: satellite-registry
            port:
              number: 5000
```

## Configuration

### Helm Values

Ground Control values.yaml:

```yaml
# Harbor configuration
harbor:
  url: "https://harbor.example.com"
  username: "robot$account"
  password: "secure-token"

# Admin configuration
adminPassword: "SecurePass123"

# Database configuration
database:
  host: "postgres"
  port: 5432
  name: "groundcontrol"
  username: "groundcontrol"
  password: "secure-password"

# Session configuration
session:
  durationHours: 24

# Security configuration
security:
  lockoutDurationMinutes: 15

# Ingress configuration
ingress:
  enabled: true
  className: "nginx"
  host: "ground-control.example.com"
  tls:
    enabled: true
    secretName: "ground-control-tls"

# Resource limits
resources:
  requests:
    memory: "256Mi"
    cpu: "250m"
  limits:
    memory: "512Mi"
    cpu: "500m"

# Scaling
replicas: 2
```

Satellite values.yaml:

```yaml
# Ground Control configuration
groundControl:
  url: "https://ground-control.example.com"

# Satellite configuration
satellite:
  token: "your-satellite-token"
  logLevel: "info"
  jsonLogging: true

# Registry configuration
registry:
  port: 8585
  storage:
    size: "50Gi"
    className: "standard"

# Container runtime integration
containerRuntime:
  enabled: true
  type: "containerd"  # or "docker"

# Metrics
metrics:
  enabled: true
  port: 9091

# Resource limits
resources:
  requests:
    memory: "512Mi"
    cpu: "500m"
  limits:
    memory: "2Gi"
    cpu: "1000m"
```

## Security

### Network Policies

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: ground-control-netpol
  namespace: harbor-satellite
spec:
  podSelector:
    matchLabels:
      app: ground-control
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
    ports:
    - protocol: TCP
      port: 9090
  - from:
    - namespaceSelector:
        matchLabels:
          name: harbor-satellite
    ports:
    - protocol: TCP
      port: 9090
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: harbor-satellite
  - to: []
    ports:
    - protocol: TCP
      port: 5432
  - to: []
    ports:
    - protocol: TCP
      port: 443
```

### Pod Security Standards

```yaml
apiVersion: v1
kind: PodSecurityPolicy
metadata:
  name: harbor-satellite-psp
spec:
  privileged: false
  allowPrivilegeEscalation: false
  requiredDropCapabilities:
    - ALL
  volumes:
    - 'configMap'
    - 'emptyDir'
    - 'persistentVolumeClaim'
    - 'secret'
  runAsUser:
    rule: 'MustRunAsNonRoot'
  seLinux:
    rule: 'RunAsAny'
  supplementalGroups:
    rule: 'MustRunAs'
    ranges:
    - min: 1
      max: 65535
  fsGroup:
    rule: 'MustRunAs'
    ranges:
    - min: 1
      max: 65535
```

### RBAC

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: harbor-satellite-role
  namespace: harbor-satellite
rules:
- apiGroups: [""]
  resources: ["configmaps", "secrets", "services"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: harbor-satellite-rolebinding
  namespace: harbor-satellite
subjects:
- kind: ServiceAccount
  name: harbor-satellite-sa
roleRef:
  kind: Role
  name: harbor-satellite-role
  apiGroup: rbac.authorization.k8s.io
```

## Monitoring

### Prometheus ServiceMonitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: ground-control-monitor
  namespace: harbor-satellite
spec:
  selector:
    matchLabels:
      app: ground-control
  endpoints:
  - port: metrics
    path: /metrics
    interval: 30s
```

### Metrics Configuration

Enable metrics in satellite:

```yaml
env:
- name: METRICS_ENABLED
  value: "true"
- name: METRICS_PORT
  value: "9091"
```

## Scaling

### Horizontal Pod Autoscaling

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: ground-control-hpa
  namespace: harbor-satellite
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ground-control
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

### Cluster Autoscaling

For satellite nodes:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaling
metadata:
  name: satellite-hpa
  namespace: harbor-satellite
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: satellite
  minReplicas: 1
  maxReplicas: 5
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 60
```

## Backup and Recovery

### Database Backup

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: postgres-backup
  namespace: harbor-satellite
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:15-alpine
            command:
            - /bin/sh
            - -c
            - pg_dump -U groundcontrol -h postgres groundcontrol | gzip > /backup/backup-$(date +%Y%m%d-%H%M%S).sql.gz
            env:
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef:
                  name: ground-control-secrets
                  key: DB_PASSWORD
            volumeMounts:
            - name: backup-storage
              mountPath: /backup
          volumes:
          - name: backup-storage
            persistentVolumeClaim:
              claimName: backup-pvc
          restartPolicy: OnFailure
```

### Registry Backup

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: registry-backup
  namespace: harbor-satellite
spec:
  schedule: "0 3 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: alpine:latest
            command:
            - /bin/sh
            - -c
            - tar czf /backup/zot-backup-$(date +%Y%m%d-%H%M%S).tar.gz -C /zot-storage .
            volumeMounts:
            - name: zot-storage
              mountPath: /zot-storage
              readOnly: true
            - name: backup-storage
              mountPath: /backup
          volumes:
          - name: zot-storage
            persistentVolumeClaim:
              claimName: zot-pvc
          - name: backup-storage
            persistentVolumeClaim:
              claimName: backup-pvc
          restartPolicy: OnFailure
```

## Troubleshooting

### Common Issues

1. **Pod crashes**:
   ```bash
   kubectl logs -n harbor-satellite deployment/ground-control
   ```

2. **Service unreachable**:
   ```bash
   kubectl get endpoints -n harbor-satellite
   ```

3. **PVC pending**:
   ```bash
   kubectl describe pvc -n harbor-satellite
   ```

4. **Ingress issues**:
   ```bash
   kubectl describe ingress -n harbor-satellite
   ```

### Debug Commands

```bash
# Check pod status
kubectl get pods -n harbor-satellite

# Check logs
kubectl logs -f deployment/ground-control -n harbor-satellite

# Check events
kubectl get events -n harbor-satellite

# Exec into pod
kubectl exec -it deployment/ground-control -n harbor-satellite -- sh

# Check network policies
kubectl get networkpolicies -n harbor-satellite
```

## Updates

### Rolling Updates

```bash
# Update Ground Control
kubectl set image deployment/ground-control ground-control=harbor-satellite/ground-control:v1.1.0 -n harbor-satellite

# Update Satellite
kubectl set image deployment/satellite satellite=harbor-satellite/satellite:v1.1.0 -n harbor-satellite
```

### Helm Upgrades

```bash
helm upgrade ground-control harbor-satellite/ground-control \
  --namespace harbor-satellite \
  --version 1.1.0

helm upgrade satellite harbor-satellite/satellite \
  --namespace harbor-satellite \
  --version 1.1.0
```</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/deployment/kubernetes.md