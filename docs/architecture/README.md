# Harbor Satellite Architecture

This document describes the core architecture and implementation details of Harbor Satellite.

## System Design

### Component Overview

1. **Ground Control**
   - Central management service
   - REST API for satellite management
   - State synchronization
   - Group-based artifact management
   - Authentication and authorization

2. **Satellite**
   - Single-process edge component
   - State replication
   - Configuration management
   - Zero-touch registration
   - Container runtime integration

3. **Local Registry**
   - OCI-compliant storage
   - Local caching
   - Artifact management
   - Access control

## Implementation Details

### Core Functionality

1. **State Management**
   - JSON-based configuration
   - Three main jobs:
     - `replicate_state`: Synchronizes state with Ground Control
     - `update_config`: Updates satellite configuration
     - `register_satellite`: Handles satellite registration
   - Cron-based scheduling
   - Event-driven updates

2. **Container Runtime Integration**
   - Automatic configuration for:
     - containerd
     - CRI-O
   - Runtime-specific settings
   - Mirror registry management

3. **Security Implementation**
   - Token-based authentication
   - Group-based access control
   - Secure communication channels
   - Credential management

## Current Limitations

1. **Feature Scope**
   - Limited to OCI-compliant registries
   - Basic monitoring capabilities
   - Manual configuration for some features

2. **Performance Considerations**
   - Network bandwidth usage
   - Storage requirements
   - Processing overhead

## Future Considerations

1. **Planned Improvements**
   - Enhanced monitoring
   - Advanced security features
   - Performance optimizations

2. **Potential Extensions**
   - Additional runtime support
   - Advanced caching
   - Distributed deployment 