# AI Gateway MVP

Go/Gin MVP for a multi-tenant AI Gateway. It issues Gateway API keys, validates tenant/key scopes, proxies OpenAI-style chat completion requests to a mock provider, and records usage by tenant, key, model, and optional caller-owned `thread_id`.

## Why This Shape

This project borrows the practical parts of `songquanpeng/one-api`: API keys are separate from upstream provider keys, chat completion proxying is stateless, model permission is checked before routing, and usage is logged after a successful response.

I intentionally did not fork One API for this take-home task. One API is a full product with users, channels, quota billing, UI, and many providers. The assignment asks for a small Go MVP that is easy to review in a few minutes, so this repo keeps the surface focused on tenant/key management, mock proxying, usage tracking, OpenAPI, and Docker.

## Architecture

```text
Caller
  Authorization: Bearer sk-gw_xxx
        |
        v
AI Gateway
  validate key hash, enabled, expiry
  validate tenant scopes and key scopes
  call mock provider
  record usage
        |
        v
Mock model provider
```

Tenant scopes define the maximum allowed capabilities. API key scopes must be a subset of tenant scopes. At request time both levels are checked.

Example scopes:

```text
chat:invoke
model:mock-gpt
model:mock-error
model:mock-timeout
model:*
*
```

`/v1/chat/completions` is stateless, matching the common OpenAI-compatible proxy model. The caller owns conversation history and sends the full `messages` array each time. `thread_id` is optional and is recorded only for usage attribution.

## Assumptions

- This MVP optimizes for local review and a four-hour implementation window, so management APIs are open and the model provider is mocked.
- Gateway API keys are different from upstream provider keys. Callers only receive Gateway keys, and upstream provider credentials can be added later behind the provider interface.
- Scope strings are intentionally simple: `chat:invoke` controls the chat operation, `model:<name>` controls model access, `model:*` allows all configured models, and `*` allows everything.
- The gateway does not own conversation state. The caller sends the full OpenAI-style `messages` array on each request.

## Run

```bash
docker compose up --build
```

Service:

```text
http://localhost:8080
```

OpenAPI:

```text
http://localhost:8080/openapi.yaml
http://localhost:8080/docs
```

Dashboard:

```text
http://localhost:8080
http://localhost:8080/dashboard
```

The dashboard can create tenants, issue keys, enable or disable keys, send a mock chat request, and inspect usage records.

## Persistence

The service uses SQLite through the pure Go `modernc.org/sqlite` driver. By default it writes to `gateway.db` when run locally, and Docker sets `DB_PATH=/data/gateway.db` with a named volume for persistence.

Tables are created automatically on startup:

```text
tenants        tenant id, name, scopes, created_at
api_keys       key id, tenant id, name, key hash, key prefix, scopes, enabled, expires_at, created_at
usage_records  tenant id, key id, model, thread_id, prompt tokens, completion tokens, total tokens, created_at
```

## Gateway Configuration

Scope names, the model scope prefix, and mock models are configured in `gateway-config.json`, not hard-coded in the request handler. The service reads `CONFIG_PATH` on startup, defaulting to `gateway-config.json` locally and `/app/gateway-config.json` in Docker.

```json
{
  "scopes": {
    "chat_invoke": "chat:invoke",
    "model_prefix": "model:"
  },
  "models": [
    {"name": "mock-gpt", "behavior": "success"},
    {"name": "mock-error", "behavior": "error"},
    {"name": "mock-timeout", "behavior": "timeout"}
  ]
}
```

Supported mock behaviors are `success`, `error`, and `timeout`. If a tenant is created without explicit scopes, it receives the configured chat scope plus the first configured model. A request must pass both scope checks and model configuration checks; even a key with `model:*` cannot call a model that is not listed in `gateway-config.json`.

## Requirement Mapping

```text
Tenant and key management  POST /tenants, POST/GET/PATCH /tenants/{tenant_id}/keys
AI proxy                  POST /v1/chat/completions
Usage tracking            GET /usage, GET /usage/summary
OpenAPI                   GET /openapi.yaml, GET /docs
Dashboard                 GET /, GET /dashboard
One-command runtime       docker compose up --build
Error handling            401 invalid/disabled/expired key, 403 scope denied, 502/504 mock upstream failures
```

## Curl Flow

The examples below use `jq` for shell JSON extraction. If `jq` is not installed, copy the `id` and `key` fields from the JSON responses manually.

Create a tenant:

```bash
TENANT_ID=$(curl -s http://localhost:8080/tenants \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "acme",
    "scopes": ["chat:invoke", "model:mock-gpt", "model:mock-error", "model:mock-timeout"]
  }' | jq -r .id)
echo "$TENANT_ID"
```

Create a Gateway key. The raw key is returned only once:

```bash
KEY_RESPONSE=$(curl -s http://localhost:8080/tenants/$TENANT_ID/keys \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "dev-service",
    "scopes": ["chat:invoke", "model:mock-gpt"],
    "enabled": true,
    "expires_at": "2026-12-31T23:59:59Z"
  }')

GATEWAY_KEY=$(echo "$KEY_RESPONSE" | jq -r .key)
KEY_ID=$(echo "$KEY_RESPONSE" | jq -r .id)
echo "$GATEWAY_KEY"
```

Call the OpenAI-style proxy:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "mock-gpt",
    "thread_id": "thread-demo-1",
    "messages": [
      {"role": "system", "content": "You are concise."},
      {"role": "user", "content": "hello gateway"}
    ]
  }' | jq
```

Query usage:

```bash
curl -s "http://localhost:8080/usage?tenant_id=$TENANT_ID&api_key_id=$KEY_ID" | jq
curl -s "http://localhost:8080/usage/summary?tenant_id=$TENANT_ID" | jq
```

Verify forbidden model access. This key can use `mock-gpt` only, so this returns `403`:

```bash
curl -i http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "mock-error",
    "messages": [{"role": "user", "content": "should be forbidden"}]
  }'
```

Disable the key and verify `401`:

```bash
curl -s -X PATCH http://localhost:8080/tenants/$TENANT_ID/keys/$KEY_ID \
  -H 'Content-Type: application/json' \
  -d '{"enabled": false}' | jq

curl -i http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"mock-gpt","messages":[{"role":"user","content":"disabled key"}]}'
```

To test upstream `502` and `504`, create a key with `model:mock-error` or `model:mock-timeout` scope, then call those models.

## Self-check

```bash
go test ./...
docker compose up --build
curl -fsS http://localhost:8080/healthz
```

Then run the curl flow above to verify tenant creation, key creation, OpenAI-style chat proxying, forbidden model access, disabled key rejection, and usage queries.

## Main Endpoints

```text
POST  /tenants
GET   /tenants
POST  /tenants/{tenant_id}/keys
GET   /tenants/{tenant_id}/keys
PATCH /tenants/{tenant_id}/keys/{key_id}
POST  /v1/chat/completions
GET   /usage
GET   /usage/summary
GET   /
GET   /dashboard
GET   /config
GET   /openapi.yaml
GET   /docs
```

## Design Decisions

- Raw Gateway keys are never stored. The DB stores SHA-256 hashes plus a public prefix for display.
- Tenant scopes are the upper bound. Key scopes must be a subset, and both are checked at request time in case tenant permissions change later.
- Token counting is approximate for the mock provider: max of word count and `rune_count / 4`, plus a small per-message overhead.
- Usage is recorded as immutable request records, not pre-aggregated counters. Tenant/key/model totals are computed with queries.
- Scope names, model scope prefix, model names, and mock behavior live in `gateway-config.json`, keeping policy configuration outside the Go request path.
- The mock provider keeps the assignment runnable without real OpenAI/Claude/DeepSeek credentials. A real provider can be added behind `proxy.Provider`.
- SQLite is used for a simple one-command MVP. For high concurrency, switch to Postgres/MySQL and add Redis-backed key/quota caching, similar to One API's production-oriented approach.
- The dashboard is served as embedded static HTML from the Go binary, avoiding a separate Node build or extra container.

## Known Limits

- Management APIs are intentionally unauthenticated for local review. Production should add admin auth.
- Streaming responses are rejected with `400`; non-streaming chat completions are implemented.
- There is no quota enforcement yet, only usage recording.
- No service-owned thread/message store. Callers own chat history and may pass `thread_id` for attribution.
- Model configuration is loaded at startup; there is no hot reload or admin UI for model changes yet.
- No real upstream model provider is wired in this MVP.
