# Email Check API

Dockerized Go API wrapper around the Reacher `check-if-email-exists` backend.

The upstream backend runs as `reacherhq/backend:latest` and exposes `POST /v0/check_email`. This service provides a smaller public API surface and forwards requests to Reacher inside Docker Compose.

## Run

```sh
docker compose up --build
```

The API listens on `http://localhost:8080`.

Scalar API documentation is available at `http://localhost:8080/`.
The OpenAPI document is available at `http://localhost:8080/openapi.json`.

> Reacher performs SMTP checks, so the host running Docker needs outbound SMTP access, including port 25, for full verification behavior.

## Endpoints

### `GET /healthz`

```sh
curl http://localhost:8080/healthz
```

### `POST /v1/check`

```sh
curl -X POST http://localhost:8080/v1/check \
  -H 'Content-Type: application/json' \
  -d '{"email":"someone@gmail.com"}'
```

Response:

```json
{
  "email": "someone@gmail.com",
  "result": {
    "input": "someone@gmail.com",
    "is_reachable": "invalid",
    "misc": {},
    "mx": {},
    "smtp": {},
    "syntax": {}
  }
}
```

### `POST /v1/check/batch`

```sh
curl -X POST http://localhost:8080/v1/check/batch \
  -H 'Content-Type: application/json' \
  -d '{"emails":["first@example.com","second@example.com"]}'
```

## Rate Limiting

The API allows 60 requests per minute per IP. Rate limit state is tracked with a fixed-window algorithm. Responses include standard headers:

```http
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 58
X-RateLimit-Reset: 1746392460
```

When the limit is exceeded, the API returns `429 Too Many Requests` with a `Retry-After` header indicating seconds until the window resets.

## Proxy

Both check endpoints accept Reacher's optional SOCKS5 proxy payload:

```json
{
  "email": "someone@gmail.com",
  "proxy": {
    "host": "my-proxy.io",
    "port": 1080,
    "username": "me",
    "password": "pass"
  }
}
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | API port inside the container. |
| `REACHER_BACKEND_URL` | `http://reacher:8080` | Base URL for the Reacher backend. |
| `HTTP_TIMEOUT_SECONDS` | `30` | Timeout for calls to Reacher. |
| `MAX_BATCH_SIZE` | `25` | Maximum number of emails accepted by `/v1/check/batch`. |
| `RATE_LIMIT_PER_MINUTE` | `60` | Maximum requests per fixed one-minute window per IP. |

## License Note

Reacher is dual licensed. Its README states that open-source usage is under AGPL-3.0 and proprietary commercial usage requires a commercial license. Review the upstream license before deploying this wrapper in a commercial product.
