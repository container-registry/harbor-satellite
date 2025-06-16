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
