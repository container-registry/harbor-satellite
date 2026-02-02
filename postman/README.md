# Harbor Satellite Ground Control API Collection

This Postman collection provides comprehensive API endpoints for testing the Harbor Satellite Ground Control server.

## Setup

1. Import `ground_control_api_collection.postman_collection.json` into Postman
2. Ensure the Ground Control server is running on `http://localhost:9090` (or update `{{base_url}}` variable)
3. Start with public endpoints (Health Check, Login)
4. Use the Login endpoint to obtain an auth token and set the `{{auth_token}}` variable
5. Click on 'Harbor Satellite Ground Control API' folder in the left sidebar of postman
6. Click on 'Authorize' tab in the top bar.
7. Now paste the token in the 'Token' field and `Ctrl+S` to save.
8. You can now access authenticated endpoints as well.

## Usage

- **Public Endpoints**: Health checks and authentication
- **Authenticated Endpoints**: Require Bearer token in Authorization header
- **Satellite Endpoints**: Manage satellite configurations and sync operations

## Prerequisites

- Ground Control server running locally
- Postman application installed