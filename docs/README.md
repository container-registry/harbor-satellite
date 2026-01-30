# Harbor Satellite Documentation

Welcome to the Harbor Satellite documentation! This comprehensive guide will help you understand, deploy, and manage Harbor Satellite in your edge computing environment.

## Table of Contents

### Getting Started
- **[Quick Start Guide](getting-started.md)** - Step-by-step setup for Ground Control and Satellite
- **[QuickStart](https://github.com/container-registry/harbor-satellite/blob/main/QUICKSTART.md)** - Basic setup instructions

### Configuration
- **[Configuration Reference](configuration.md)** - Complete configuration options for all components
- **[Environment Variables](configuration.md#ground-control-configuration)** - Ground Control settings
- **[Satellite Configuration](configuration.md#satellite-configuration)** - Satellite and Zot settings

### Architecture & Design
- **[System Architecture](architecture.md)** - High-level system overview
- **[Use Cases](docs/architecture/use-cases.md)** - Deployment patterns and scenarios
- **[Components](docs/architecture/components.md)** - Detailed component descriptions

### API & Integration
- **[API Reference](api-reference.md)** - Complete Ground Control REST API
- **[Authentication](api-reference.md#authentication)** - API authentication methods
- **[Endpoints](api-reference.md#protected-endpoints)** - All available API endpoints

### Deployment
- **[Docker Deployment](deployment/docker.md)** - Docker Compose and container deployment
- **[Kubernetes Deployment](deployment/kubernetes.md)** - Helm charts and K8s manifests
- **[Production Setup](deployment/docker.md#production-deployment)** - Production-ready configurations

### Operations
- **[Troubleshooting](troubleshooting.md)** - Common issues and solutions
- **[Monitoring](deployment/docker.md#monitoring-and-logging)** - Health checks and metrics
- **[Backup & Recovery](deployment/docker.md#backup-and-recovery)** - Data protection strategies

### Development
- **[Architecture Decisions](docs/decisions/)** - Design decisions and rationale
- **[Contributing](https://github.com/container-registry/harbor-satellite/blob/main/CONTRIBUTING.md)** - Development guidelines

## Overview

Harbor Satellite brings Harbor container registry capabilities to edge computing environments. It consists of:

- **Ground Control**: Central management service for orchestrating satellite deployments
- **Satellite**: Edge registry that caches and serves container images locally
- **Zot Registry**: OCI-compliant registry for local artifact storage

## Key Features

- **Edge-First Design**: Optimized for unreliable network conditions
- **Air-Gapped Support**: Operates without internet connectivity
- **Centralized Management**: Single pane of glass for fleet management
- **Container Runtime Integration**: Automatic mirror configuration
- **Artifact Synchronization**: Intelligent caching and replication

## Quick Links

- [GitHub Repository](https://github.com/container-registry/harbor-satellite)
- [Issue Tracker](https://github.com/container-registry/harbor-satellite/issues)
- [CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1)
- [Harbor Project](https://goharbor.io)

## Support

- **Documentation Issues**: [Open an issue](https://github.com/container-registry/harbor-satellite/issues/new?labels=documentation)
- **Community Support**: [#harbor-satellite on CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1)
- **Commercial Support**: Contact the [Harbor team](https://goharbor.io/contact/)

## Contributing to Documentation

We welcome contributions to improve our documentation! Please see our [contributing guidelines](https://github.com/container-registry/harbor-satellite/blob/main/CONTRIBUTING.md) for details.

To contribute:
1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

---

*Last updated: January 30, 2026*</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/README.md