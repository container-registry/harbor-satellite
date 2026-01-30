# Troubleshooting

This guide helps diagnose and resolve common issues with Harbor Satellite deployments.

## Ground Control Issues

### Service Won't Start

**Symptoms:**
- `docker compose up` fails
- Port 9090 is not accessible
- Database connection errors

**Solutions:**

1. **Check Docker Compose Logs**:
   ```bash
   cd ground-control
   docker compose logs
   ```

2. **Verify Environment Variables**:
   ```bash
   cat .env
   # Ensure all required variables are set
   ```

3. **Check Database Connectivity**:
   ```bash
   docker compose exec postgres pg_isready -h localhost
   ```

4. **Verify Port Availability**:
   ```bash
   netstat -tlnp | grep 9090
   ```

### Authentication Failures

**Symptoms:**
- Login returns 401 Unauthorized
- API calls fail with authentication errors

**Solutions:**

1. **Check Password Policy**:
   - Default requires: 8+ chars, uppercase, lowercase, number
   - Verify `ADMIN_PASSWORD` meets requirements

2. **Validate Credentials**:
   ```bash
   curl -X POST http://localhost:9090/login \
     -H "Content-Type: application/json" \
     -d '{"username": "admin", "password": "your-password"}'
   ```

3. **Check Session Duration**:
   - Tokens expire after `SESSION_DURATION_HOURS` (default: 24)

### Database Issues

**Symptoms:**
- "connection refused" errors
- Migration failures
- Data inconsistency

**Solutions:**

1. **Reset Database**:
   ```bash
   cd ground-control
   docker compose down -v
   docker compose up -d
   ```

2. **Check Database Logs**:
   ```bash
   docker compose logs postgres
   ```

3. **Manual Migration**:
   ```bash
   docker compose exec ground-control ./migrator
   ```

## Satellite Issues

### Registration Failures

**Symptoms:**
- Satellite registration returns errors
- Token not received

**Solutions:**

1. **Verify Ground Control Connectivity**:
   ```bash
   curl http://ground-control:9090/health
   ```

2. **Check Group and Config Existence**:
   ```bash
   curl http://localhost:9090/groups -H "Authorization: Bearer $TOKEN"
   curl http://localhost:9090/configs -H "Authorization: Bearer $TOKEN"
   ```

3. **Validate Configuration JSON**:
   - Ensure `ground_control_url` is correct
   - Check `local_registry.url` format

### Replication Issues

**Symptoms:**
- Artifacts not syncing
- "failed to pull" errors
- Storage not updating

**Solutions:**

1. **Check Harbor Connectivity**:
   ```bash
   curl https://harbor.example.com/api/v2.0/health
   ```

2. **Verify Robot Account**:
   - Ensure robot account exists in Harbor
   - Check permissions (pull access to specified projects)

3. **Validate Artifact Configuration**:
   ```bash
   # Check digest format
   docker inspect harbor.example.com/library/alpine:latest | grep RepoDigests
   ```

4. **Review Satellite Logs**:
   ```bash
   docker compose logs satellite
   ```

### Local Registry Issues

**Symptoms:**
- Zot registry not accessible
- Port 8585 not responding
- Image push/pull failures

**Solutions:**

1. **Check Zot Configuration**:
   ```bash
   curl http://localhost:8585/v2/
   ```

2. **Verify Storage Permissions**:
   ```bash
   ls -la ./zot/
   # Ensure write permissions
   ```

3. **Check Port Conflicts**:
   ```bash
   netstat -tlnp | grep 8585
   ```

4. **Restart Registry**:
   ```bash
   docker compose restart satellite
   ```

## Container Runtime Issues

### Mirror Configuration Failures

**Symptoms:**
- Containerd/docker can't pull from mirrors
- Images still pulling from upstream

**Solutions:**

1. **Check Mirror Setup**:
   ```bash
   # For containerd
   cat /etc/containerd/config.toml | grep mirror

   # For Docker
   cat /etc/docker/daemon.json
   ```

2. **Restart Container Runtime**:
   ```bash
   # Docker
   systemctl restart docker

   # Containerd
   systemctl restart containerd
   ```

3. **Verify Satellite Permissions**:
   - Satellite needs sudo access for configuration
   - Check if `--mirrors` flag was used correctly

### Image Pull Issues

**Symptoms:**
- `docker pull` fails
- "manifest unknown" errors

**Solutions:**

1. **Check Local Registry**:
   ```bash
   curl http://localhost:8585/v2/library/alpine/manifests/latest
   ```

2. **Verify Artifact Sync**:
   ```bash
   # Check if artifact was replicated
   curl http://localhost:9090/groups/my-group \
     -H "Authorization: Bearer $TOKEN"
   ```

3. **Test Direct Pull**:
   ```bash
   docker pull localhost:8585/library/alpine:latest
   ```

## Network Issues

### Connectivity Problems

**Symptoms:**
- Timeouts connecting to Ground Control
- DNS resolution failures
- Firewall blocking connections

**Solutions:**

1. **Test Network Connectivity**:
   ```bash
   ping ground-control-host
   telnet ground-control-host 9090
   ```

2. **Check DNS Resolution**:
   ```bash
   nslookup harbor.example.com
   ```

3. **Verify Firewall Rules**:
   ```bash
   iptables -L
   # Check for blocked ports
   ```

4. **Use Correct URLs**:
   - For Docker: use `host.docker.internal`
   - For Kubernetes: use service names
   - For remote: use actual IP/hostname

### SSL/TLS Issues

**Symptoms:**
- Certificate validation errors
- "tls: bad certificate" messages

**Solutions:**

1. **Disable SSL Verification** (development only):
   ```json
   {
     "use_unsecure": true
   }
   ```

2. **Add CA Certificate**:
   ```bash
   # Copy CA cert to system store
   cp ca.crt /usr/local/share/ca-certificates/
   update-ca-certificates
   ```

3. **Check Certificate Validity**:
   ```bash
   openssl s_client -connect harbor.example.com:443
   ```

## Performance Issues

### High Resource Usage

**Symptoms:**
- High CPU/memory consumption
- Slow replication
- Storage filling up

**Solutions:**

1. **Adjust Intervals**:
   ```json
   {
     "state_replication_interval": "@every 00h05m00s",
     "heartbeat_interval": "@every 00h01m00s"
   }
   ```

2. **Enable Metrics**:
   ```json
   {
     "metrics": {
       "collect_cpu": true,
       "collect_memory": true,
       "collect_storage": true
     }
   }
   ```

3. **Configure Storage Cleanup**:
   ```json
   {
     "zot_config": {
       "storage": {
         "gc": true,
         "dedupe": true
       }
     }
   }
   ```

### Slow Synchronization

**Symptoms:**
- Long time to sync artifacts
- Bandwidth saturation

**Solutions:**

1. **Check Network Bandwidth**:
   ```bash
   speedtest-cli
   ```

2. **Optimize Artifact Selection**:
   - Only sync required tags
   - Use specific digests instead of tags

3. **Parallel Downloads**:
   - Satellite supports concurrent pulls
   - Check `max_concurrent_downloads` in Zot config

## Monitoring and Debugging

### Enable Debug Logging

```json
{
  "log_level": "debug"
}
```

### Check Logs

```bash
# Ground Control
docker compose logs ground-control

# Satellite
docker compose logs satellite

# Database
docker compose logs postgres
```

### Health Endpoints

```bash
# Ground Control
curl http://localhost:9090/health

# Satellite registry
curl http://localhost:8585/v2/
```

### Common Log Messages

- `"failed to authenticate"`: Check credentials
- `"connection refused"`: Check network connectivity
- `"storage full"`: Clean up disk space
- `"invalid digest"`: Verify artifact configuration

## Getting Help

If issues persist:

1. **Check Existing Issues**:
   - [GitHub Issues](https://github.com/container-registry/harbor-satellite/issues)

2. **Community Support**:
   - [#harbor-satellite on CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1)

3. **Documentation**:
   - [Harbor Satellite Docs](https://docs.goharbor.io)
   - [Zot Registry Docs](https://zotregistry.io)

4. **Debug Information**:
   ```bash
   # System info
   uname -a
   docker --version

   # Satellite version
   docker compose exec satellite ./satellite --version

   # Configuration dump
   curl http://localhost:9090/satellites/my-satellite/status \
     -H "Authorization: Bearer $TOKEN"
   ```</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/troubleshooting.md