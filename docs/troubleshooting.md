# Harbor Satellite Troubleshooting Guide

This guide helps you diagnose and resolve common issues with Harbor Satellite deployment and operation.

## Quick Diagnostic Commands

Before diving into specific issues, run these commands to gather system state:

```bash
# Check satellite service status
systemctl status satellite

# View recent satellite logs
journalctl -u satellite -f --since "10 minutes ago"

# Test Ground Control connectivity
curl -f http://your-ground-control:8080/health

# Check local registry status
curl -f http://localhost:8585/v2/

# Verify container runtime configuration
# For Docker:
docker info | grep -A 10 "Registry Mirrors"
# For containerd:
crictl info | grep -A 5 "registry"
```

## Common Issues

### Satellite Startup Problems

#### Satellite fails to start with configuration errors

**Symptoms:**
- Application exits during startup
- "Configuration file not found" or similar errors
- Exit code 1 or 2

**Diagnostic Commands:**
```bash
# Check if config file exists and is readable
ls -la /path/to/config.json
satellite --config /path/to/config.json --validate-config
```

**Causes & Solutions:**

1. **Missing configuration file**
   ```bash
   # Check file permissions
   chmod 644 /path/to/config.json
   # Verify satellite user can read the file
   sudo -u satellite cat /path/to/config.json
   ```

2. **Invalid JSON syntax**
   ```bash
   # Validate JSON syntax
   cat /path/to/config.json | jq .
   # If jq is not available:
   python3 -m json.tool /path/to/config.json
   ```

3. **Wrong working directory**
   ```bash
   # Run with absolute paths
   satellite --config /absolute/path/to/config.json
   # Or set working directory
   cd /opt/satellite && satellite --config ./config.json
   ```

#### Satellite exits with "Error initiating the config manager"

**Symptoms:**
- Process starts but exits immediately with configuration errors
- HTTP client initialization failures

**Diagnostic Commands:**
```bash
# Test Ground Control connectivity manually
curl -v http://your-ground-control:8080/health
# Check DNS resolution
nslookup your-ground-control
dig your-ground-control
```

**Solutions:**

1. **Network connectivity issues**
   ```bash
   # Test basic connectivity
   ping your-ground-control
   # Test specific port
   telnet your-ground-control 8080
   nc -zv your-ground-control 8080
   ```

2. **SSL/TLS certificate problems**
   ```bash
   # For HTTPS endpoints, verify certificates
   openssl s_client -connect your-ground-control:443 -showcerts
   # Check certificate validity
   curl -v --cacert /path/to/ca.crt https://your-ground-control:8080
   ```

### Ground Control Connection Issues

#### "connection refused" when connecting to Ground Control

**Symptoms:**
- Satellite cannot reach Ground Control
- Network timeouts or connection errors
- HTTP status 500 or connection timeouts

**Diagnostic Commands:**
```bash
# Check if Ground Control is running
curl -f http://ground-control:8080/health
systemctl status ground-control

# Test network connectivity
ping ground-control
telnet ground-control 8080
nc -zv ground-control 8080

# Check for proxy interference
curl -x "" http://ground-control:8080/health
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy
```

**Debugging steps:**

1. **Verify Ground Control is running**
   ```bash
   # Check Ground Control service status
   systemctl status ground-control
   # Check if process is listening on correct port
   netstat -tlnp | grep :8080
   lsof -i :8080
   ```

2. **Check Ground Control logs**
   ```bash
   # View recent Ground Control logs
   journalctl -u ground-control -f --since "10 minutes ago"
   # Check for database connection issues
   grep -i "database\|postgres\|connection" /var/log/ground-control.log
   ```

3. **Verify database connectivity**
   ```bash
   # Test PostgreSQL connection (adjust parameters)
   psql -h localhost -U ground_control -d ground_control_db -c "SELECT 1;"
   # Check database service
   systemctl status postgresql
   ```

4. **Network troubleshooting**
   ```bash
   # Check routing
   traceroute ground-control
   # Test from satellite host
   curl -v http://ground-control:8080/api/v1/satellites
   ```

#### Authentication and authorization errors

**Symptoms:**
- HTTP 401 (Unauthorized) responses
- HTTP 403 (Forbidden) responses
- "invalid token" or "token expired" errors

**Diagnostic Commands:**
```bash
# Check current token validity
curl -H "Authorization: Bearer YOUR_TOKEN" http://ground-control:8080/api/v1/satellites

# Generate new authentication token
curl -X POST -H "Content-Type: application/json" \
  -d '{"username":"your-user","password":"your-password"}' \
  http://ground-control:8080/api/v1/auth/login

# Verify token format
echo "YOUR_TOKEN" | base64 -d
```

**Solutions:**

1. **Token expiration**
   ```bash
   # Check token expiration time
   jwt decode YOUR_TOKEN  # if jwt CLI tool is available
   # Or use online JWT decoder
   echo "Token expiry: $(curl -s -H 'Authorization: Bearer YOUR_TOKEN' http://ground-control:8080/api/v1/auth/validate | jq -r .expires_at)"
   ```

2. **Invalid permissions**
   ```bash
   # Check user permissions
   curl -H "Authorization: Bearer YOUR_TOKEN" \
     http://ground-control:8080/api/v1/auth/user
   # Verify satellite registration
   curl -H "Authorization: Bearer YOUR_TOKEN" \
     http://ground-control:8080/api/v1/satellites
   ```

3. **Clock synchronization issues**
   ```bash
   # Check system time on both satellite and Ground Control
   date
   # Check NTP synchronization
   timedatectl status
   # Synchronize if needed
   sudo timedatectl set-ntp true
   ```

### Registry Issues

#### Local registry not accessible

**Symptoms:**
- Applications cannot pull images from local satellite registry
- Registry connection timeouts or errors
- HTTP 500 errors when accessing registry endpoints

**Diagnostic Commands:**
```bash
# Check if Zot registry is running
curl -f http://localhost:8585/v2/
systemctl status zot-registry

# Test registry endpoints
curl -f http://localhost:8585/v2/_catalog
curl -f http://localhost:8585/v2/{repository}/tags/list

# Check registry process and ports
netstat -tlnp | grep :8585
lsof -i :8585
ps aux | grep zot
```

**Debugging:**

1. **Registry service not running**
   ```bash
   # Check registry service status
   systemctl status zot-registry
   # Start if not running
   systemctl start zot-registry
   # Check registry logs
   journalctl -u zot-registry -f --since "10 minutes ago"
   ```

2. **Port conflicts**
   ```bash
   # Check what's using the port
   lsof -i :8585
   netstat -tlnp | grep :8585
   # Change port if needed (update config.json)
   ```

3. **Storage permissions**
   ```bash
   # Check registry storage directory permissions
   ls -la /path/to/registry/storage/
   # Fix permissions if needed
   chown -R satellite:satellite /path/to/registry/storage/
   chmod -R 755 /path/to/registry/storage/
   ```

4. **Configuration issues**
   ```bash
   # Validate registry configuration
   cat /etc/zot/config.json | jq .
   # Test configuration
   zot serve /etc/zot/config.json --validate-config
   ```

#### Image synchronization failures

**Symptoms:**
- Images not appearing in local registry
- Sync process reports failures
- "image not found" errors despite being in desired state

**Diagnostic Commands:**
```bash
# Check satellite sync logs
grep -i "sync\|pull\|image" /var/log/satellite.log
journalctl -u satellite -f | grep -i sync

# Verify desired state from Ground Control
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://ground-control:8080/api/v1/satellites/YOUR_SATELLITE_ID/state

# Check local registry contents
curl http://localhost:8585/v2/_catalog | jq .
```

**Solutions:**

1. **Network connectivity to upstream registries**
   ```bash
   # Test connectivity to Docker Hub
   curl -f https://registry-1.docker.io/v2/
   # Test connectivity to custom registry
   curl -f https://your-registry.com/v2/
   # Check proxy settings
   echo $HTTP_PROXY $HTTPS_PROXY
   ```

2. **Authentication to upstream registries**
   ```bash
   # Test registry authentication
   docker login your-registry.com
   # Verify stored credentials
   cat ~/.docker/config.json
   # For skopeo (used by satellite):
   skopeo login your-registry.com
   ```

3. **Image manifest issues**
   ```bash
   # Check if image exists upstream
   skopeo inspect docker://upstream-registry/image:tag
   # Check manifest format compatibility
   docker manifest inspect upstream-registry/image:tag
   ```

4. **Storage space issues**
   ```bash
   # Check available disk space
   df -h /path/to/registry/storage
   # Check for sufficient inodes
   df -i /path/to/registry/storage
   # Clean up if needed
   docker system prune -af
   ```

### Container Runtime Integration

#### Containers still pulling from remote registries

**Symptoms:**
- Images downloading despite local registry running
- Slow container startup times
- Network traffic to remote registries
- Registry mirror not being used

**Diagnostic Commands:**
```bash
# For Docker:
docker info | grep -A 10 "Registry Mirrors"
cat /etc/docker/daemon.json

# For containerd:
crictl info | grep -A 5 registry
cat /etc/containerd/config.toml | grep -A 10 registry

# For CRI-O:
crictl info | grep -A 5 registry  
cat /etc/containers/registries.conf
```

**Solutions:**

1. **Verify container runtime mirror configuration**
   
   **Docker:**
   ```bash
   # Check current daemon.json
   cat /etc/docker/daemon.json
   # Should contain:
   # {
   #   "registry-mirrors": ["http://localhost:8585"]
   # }
   
   # Restart Docker daemon
   systemctl restart docker
   # Verify configuration took effect
   docker info | grep -A 5 "Registry Mirrors"
   ```

   **containerd:**
   ```bash
   # Check containerd config
   cat /etc/containerd/config.toml
   # Look for registry.mirrors section
   
   # Restart containerd
   systemctl restart containerd
   # Test with crictl
   crictl pull localhost:8585/hello-world:latest
   ```

   **CRI-O:**
   ```bash
   # Check registries configuration
   cat /etc/containers/registries.conf
   # Should have mirror configuration
   
   # Restart CRI-O
   systemctl restart crio
   ```

2. **Network connectivity between runtime and registry**
   ```bash
   # Test from container runtime perspective
   # For Docker:
   docker run --rm alpine wget -qO- http://localhost:8585/v2/
   
   # For containerd/CRI-O:
   crictl run --rm alpine wget -qO- http://host.docker.internal:8585/v2/
   ```

3. **Container runtime DNS resolution**
   ```bash
   # Test DNS resolution from containers
   docker run --rm alpine nslookup localhost
   docker run --rm alpine wget -qO- http://host.docker.internal:8585/v2/
   ```

#### Mirror configuration not working

**Symptoms:**
- Registry mirrors configured but not used
- Images still being pulled from upstream
- No traffic to local registry

**Diagnostic Commands:**
```bash
# Enable Docker daemon debug logging
sudo mkdir -p /etc/systemd/system/docker.service.d
echo '[Service]
Environment="DOCKERD_OPTS=--debug"' | sudo tee /etc/systemd/system/docker.service.d/debug.conf
sudo systemctl daemon-reload
sudo systemctl restart docker

# Monitor registry access
sudo tcpdump -i any port 8585
# Monitor upstream registry access  
sudo tcpdump -i any port 443 | grep registry

# Test mirror with specific image
docker pull hello-world
# Check which registry was actually used in logs
journalctl -u docker -f
```

**Solutions:**

1. **Registry mirror precedence**
   ```bash
   # Verify mirror order in daemon.json
   # Mirrors are tried in order listed
   {
     "registry-mirrors": [
       "http://localhost:8585",
       "https://mirror.gcr.io"
     ]
   }
   ```

2. **Authentication with mirrors**
   ```bash
   # Configure authentication for mirrors if needed
   docker login localhost:8585
   # Verify stored credentials
   cat ~/.docker/config.json | jq '.auths'
   ```

3. **Image naming and mirror routing**
   ```bash
   # Test specific registry routing
   docker pull localhost:8585/library/hello-world:latest
   # vs
   docker pull hello-world:latest  # Should use mirror
   ```

## Advanced Debugging

### Enable Debug Logging

**Satellite Debug Logging:**
```bash
# Enable debug logging in satellite config.json
{
  "log_level": "debug",
  "log_format": "json"
}

# Or use environment variable
export LOG_LEVEL=debug
satellite --config config.json

# View detailed logs
journalctl -u satellite -f | jq .
```

**Ground Control Debug Logging:**
```bash
# Set environment for debug logging
export LOG_LEVEL=debug
export GIN_MODE=debug

# View with structured output
journalctl -u ground-control -f | jq -r '.[].message'
```

**Registry Debug Logging:**
```bash
# Enable debug in Zot registry config
{
  "log": {
    "level": "debug",
    "output": "/var/log/zot.log"
  }
}
```

### Performance and Resource Monitoring

**System Resource Usage:**
```bash
# CPU and memory usage
top -p $(pgrep satellite)
top -p $(pgrep ground-control)

# Disk I/O monitoring
iotop -p $(pgrep satellite)

# Network monitoring
nethogs
iftop

# Registry storage usage
du -sh /path/to/registry/storage/*
```

**Database Performance:**
```bash
# PostgreSQL connection monitoring
sudo -u postgres psql -c "SELECT * FROM pg_stat_activity WHERE datname='ground_control_db';"

# Check for locks
sudo -u postgres psql -c "SELECT * FROM pg_locks WHERE NOT granted;"

# Database size and statistics
sudo -u postgres psql -c "\l+ ground_control_db"
sudo -u postgres psql ground_control_db -c "\dt+ public.*"
```

### State Synchronization Debugging

**Compare Local vs Desired State:**
```bash
# Get desired state from Ground Control
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://ground-control:8080/api/v1/satellites/YOUR_SATELLITE_ID/state > desired_state.json

# Get current satellite state (if available via API)
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://ground-control:8080/api/v1/satellites/YOUR_SATELLITE_ID > current_state.json

# Compare states
diff <(cat desired_state.json | jq -S .) <(cat current_state.json | jq -S .)
```

**Registry Sync Status:**
```bash
# Check what images are actually in local registry
curl http://localhost:8585/v2/_catalog | jq -r '.repositories[]' | while read repo; do
  echo "Repository: $repo"
  curl -s http://localhost:8585/v2/$repo/tags/list | jq .
done

# Compare with desired state
jq -r '.images[].name' desired_state.json | sort > desired_images.txt
curl -s http://localhost:8585/v2/_catalog | jq -r '.repositories[]' | sort > local_images.txt
diff desired_images.txt local_images.txt
```

**Sync Process Monitoring:**
```bash
# Watch sync process in real-time
journalctl -u satellite -f | grep -i "sync\|pull\|push\|image"

# Check sync timing and patterns
grep "sync" /var/log/satellite.log | tail -20 | sed 's/.*\(sync[^"]*\).*/\1/'
```

### Network Troubleshooting

**Detailed Connection Testing:**
```bash
# Test with different protocols
curl -v http://ground-control:8080/health
curl -v https://ground-control:8080/health

# Test with wget for different behavior
wget -O /dev/null -v http://ground-control:8080/health

# Use different DNS resolution
dig ground-control
nslookup ground-control
host ground-control
```

**SSL/TLS Debugging:**
```bash
# Test SSL connectivity
openssl s_client -connect ground-control:443 -servername ground-control

# Check certificate chain
openssl s_client -connect ground-control:443 -showcerts | openssl x509 -noout -text

# Verify specific cipher suites
nmap --script ssl-enum-ciphers ground-control -p 443
```

**Proxy and Firewall Debugging:**
```bash
# Test without proxy
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy
curl http://ground-control:8080/health

# Test with firewall logging (if available)
sudo iptables -I INPUT -p tcp --dport 8080 -j LOG --log-prefix "Ground-Control: "
sudo iptables -I OUTPUT -p tcp --sport 8080 -j LOG --log-prefix "Ground-Control: "
tail -f /var/log/messages | grep "Ground-Control"
```

## Log Analysis and Useful Patterns

### Important Log Patterns

**Satellite Logs:**
```bash
# Authentication issues
grep -i "auth\|401\|403\|unauthorized\|forbidden" /var/log/satellite.log

# Network connectivity issues
grep -i "connection\|refused\|timeout\|network" /var/log/satellite.log

# Sync and registry operations
grep -i "sync\|registry\|pull\|push\|image" /var/log/satellite.log

# Configuration problems
grep -i "config\|invalid\|error.*json" /var/log/satellite.log
```

**Ground Control Logs:**
```bash
# Database issues
grep -i "database\|postgres\|sql\|connection.*failed" /var/log/ground-control.log

# API request patterns
grep -E "GET|POST|PUT|DELETE" /var/log/ground-control.log | tail -20

# Authentication logs
grep -i "login\|token\|auth" /var/log/ground-control.log
```

**Registry Logs:**
```bash
# Image operations
grep -i "push\|pull\|blob\|manifest" /var/log/zot.log

# Storage operations
grep -i "storage\|filesystem\|disk" /var/log/zot.log

# Client connections
grep -E "GET|POST|PUT|DELETE" /var/log/zot.log | tail -20
```

### Log Locations by Installation Method

**Systemd Services:**
```bash
# Satellite logs
journalctl -u satellite -f --since "1 hour ago"

# Ground Control logs  
journalctl -u ground-control -f --since "1 hour ago"

# Registry logs
journalctl -u zot-registry -f --since "1 hour ago"
```

**Docker Containers:**
```bash
# Container logs
docker logs satellite -f --since 1h
docker logs ground-control -f --since 1h
docker logs zot-registry -f --since 1h

# Combined logs from docker-compose
docker-compose logs -f --tail=100
```

**Manual Installations:**
- Satellite: `/var/log/satellite.log` or configured log path
- Ground Control: `/var/log/ground-control.log` or configured log path  
- Registry: `/var/log/zot.log` or configured log path

## Recovery Procedures

### Satellite Recovery

**Complete Satellite Reset:**
```bash
# Stop satellite
systemctl stop satellite

# Clear state (if safe to do so)
rm -rf /var/lib/satellite/state/
rm -rf /var/lib/satellite/cache/

# Clear local registry storage (CAUTION: will delete all local images)
rm -rf /path/to/registry/storage/*

# Restart with clean state
systemctl start satellite
```

**Configuration Recovery:**
```bash
# Restore from backup
cp /backup/satellite-config.json /etc/satellite/config.json

# Validate restored configuration
satellite --config /etc/satellite/config.json --validate-config

# Test connectivity
curl -f http://localhost:8585/v2/
```

### Ground Control Recovery

**Database Recovery:**
```bash
# Create database backup first
sudo -u postgres pg_dump ground_control_db > ground_control_backup.sql

# Reset database (CAUTION: will lose all data)
sudo -u postgres dropdb ground_control_db
sudo -u postgres createdb ground_control_db

# Restore from backup
sudo -u postgres psql ground_control_db < ground_control_backup.sql

# Run migrations
ground-control migrate-up
```

**Configuration Recovery:**
```bash
# Restore Ground Control configuration
cp /backup/ground-control-config.json /etc/ground-control/config.json

# Validate database connection
ground-control --validate-config

# Restart service
systemctl restart ground-control
```

## Getting Help

If you can't resolve the issue:

1. **Collect debug information:**
   - Satellite and Ground Control logs
   - Configuration files (with secrets redacted)
   - System information and environment details
   
2. **Check existing issues:**
   - [GitHub Issues](https://github.com/container-registry/harbor-satellite/issues)
   
3. **Get community support:**
   - [#harbor-satellite on CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1)

## See Also

- [Getting Started Guide](getting-started.md) - Initial setup instructions
- [Configuration Reference](configuration.md) - All configuration options
- [Architecture](architecture.md) - How components interact
- [Deployment Guides](deployment/) - Platform-specific setup