# Ground Control API Development

Ground Control now follows a swagger-first workflow for generated API routes.

## Source of truth

- Edit `ground-control/api/v1/swagger.yaml` first.
- Generate `ground-control/api/v1/swagger.json` and Go server code from that spec.
- Implement generated operation handlers in `ground-control/internal/server`.
- Do not hand-edit generated files under `ground-control/internal/api/generated`.

## Workflow

1. Update `ground-control/api/v1/swagger.yaml`.
2. Run `task gen-apis`.
3. Implement or update the generated operation handlers.
4. Run `task gen-apis-check` and `go test ./...` from `ground-control`.

## Auth guidance

- Bearer auth is the recommended client flow.
- Basic auth remains supported on authenticated human endpoints for automation compatibility.
- Machine endpoints keep their own auth contracts and should be documented explicitly in the spec.
