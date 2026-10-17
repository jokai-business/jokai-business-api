# JOKAI Business API

Go backend for JOKAI Business OS. It provides organization-scoped CRM, sales quotes and approvals, delivery projects and tasks, invoices and payments, documents, notifications, Gmail connections, AI assistance, imports, and administrator settings.

This repository builds independently. The Go module is still named `ai-business-api`; keep that name in imports. Renaming the repository directory does not change the configured OIDC audience (`ai-business-api`) or existing database identifiers.

## Requirements

- Go 1.26 or newer compatible with `go.mod`.
- PostgreSQL 17 for the supported local deployment.
- An OIDC provider configured with the API audience and a reachable JWKS endpoint. The companion deployment uses Keycloak.
- A writable, absolute directory for document originals.
- Docker Compose and Python 3 if using the companion local deployment.

## Start the complete local application

Clone the three repositories into sibling directories:

```text
workspace/
  jokai-business-api/
  jokai-business-web/
  jokai-business-deployment/
```

For a fresh development environment, run:

```bash
cd ../jokai-business-deployment
python3 scripts/setup-local.py
docker compose up -d --build
python3 scripts/seed-local.py
docker compose ps
```

The deployment starts PostgreSQL, migrations, Keycloak, the API, web application, and HTTPS gateway. Open `https://localhost:8443`. Trust the generated `.local/tls/local.crt` certificate locally. Development account details are stored in `.local/accounts.json`; do not commit them. Keycloak can take about a minute to initialize.

From the deployment directory:

```bash
curl --cacert .local/tls/local.crt https://localhost:8443/health
curl --cacert .local/tls/local.crt https://localhost:8443/ready
docker compose logs --tail=100 api
```

Stop with `docker compose down`. Adding `-v` deletes persistent database and document volumes. This Compose environment is intended for local development, not public production hosting.

## Run the API directly with Go

Run these commands from this repository. First provision an empty development database and configure your OIDC realm. The companion Compose database is not published to a host port by default; use a separately reachable development database for host-based Go execution.

```bash
go mod download
mkdir -p .local/documents

export HTTP_ADDR='127.0.0.1:8080'
export DATABASE_URL='postgres://app:replace-me@127.0.0.1:5432/jokai?sslmode=disable'
export APP_URL='http://localhost:3000'
export OIDC_ISSUER='https://identity.example.com/realms/jokai'
export OIDC_JWKS_URL='https://identity.example.com/realms/jokai/protocol/openid-connect/certs'
export DOCUMENT_STORAGE_BACKEND='local'
export DOCUMENT_STORAGE_PATH="$PWD/.local/documents"

go run ./cmd/migrate
go run ./cmd/api
```

Replace database and identity-provider placeholders with your development configuration. The `sslmode=disable` example is for a local database only. The API reads process environment variables; it does **not** automatically load `.env` files. The frontend must be configured separately to reach this API and use the same identity provider.

In another terminal:

```bash
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/ready
```

`/health` checks HTTP liveness. `/ready` checks database connectivity and returns 503 when unavailable; it does not certify migrations, workers, or external integrations.

### Core configuration

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Required PostgreSQL connection string. |
| `HTTP_ADDR` | Listen address; defaults to `:8080`. |
| `APP_URL` | Required application origin. Requests with a different `Origin` header are rejected. |
| `OIDC_ISSUER` | Required expected token issuer. |
| `OIDC_JWKS_URL` | Required signing-key URL reachable by the API. |
| `DOCUMENT_STORAGE_BACKEND` | `local` (default) or `s3`. |
| `DOCUMENT_STORAGE_PATH` | Required absolute private directory, writable by the API user. Retain it when enabling S3 so older local files remain readable. |

## Provision the first administrator

A successful OIDC login does not automatically create an organization or grant access. For a new environment, create the user in your identity provider, then provision their existing subject with the bootstrap command:

```bash
export BOOTSTRAP_SUBJECT='existing-oidc-user-subject'
export BOOTSTRAP_EMAIL='admin@example.com'
export BOOTSTRAP_NAME='Organization administrator'
export BOOTSTRAP_ORGANIZATION='My organization'
go run ./cmd/bootstrap
unset BOOTSTRAP_SUBJECT BOOTSTRAP_EMAIL BOOTSTRAP_NAME BOOTSTRAP_ORGANIZATION
```

Use the migrated database through `DATABASE_URL`. Bootstrap creates an organization, administrator membership, and audit entry atomically. It is a one-time provisioning command, not an idempotent startup task; duplicate subjects are refused. The complete local setup's seed script provisions its own test accounts, so do not bootstrap those accounts again.

## Call the API

Business endpoints use `/api/v1`. Send a short-lived OIDC **access token** with the configured issuer and audience `ai-business-api`. Organization and permissions are resolved from the authenticated membership, not a caller-selected organization header.

```bash
# ACCESS_TOKEN must already contain a valid access token from your login flow.
curl --fail-with-body http://127.0.0.1:8080/api/v1/me \
  -H "Authorization: Bearer ${ACCESS_TOKEN}"

curl --fail-with-body http://127.0.0.1:8080/api/v1/customers \
  -H "Authorization: Bearer ${ACCESS_TOKEN}"
```

Use `https://localhost:8443` and `--cacert ../jokai-business-deployment/.local/tls/local.crt` instead when calling the full local stack. Never save access tokens in source control or API collections.

The canonical [OpenAPI contract](api/openapi.yaml) documents request bodies, responses, and endpoints. It uses JSON syntax, which is also valid YAML. The [Bruno collection](tests/api/README.md) contains interactive examples; open `tests/api` in Bruno and configure `baseUrl` and a secret `accessToken` variable.

Errors return an `error` object containing `code`, `message`, and `requestId`. Use that request ID to correlate with structured server logs. Missing/invalid tokens return 401; insufficient membership permissions or a disallowed origin return 403. For versioned resources, read the current version before writing and refresh after a conflict.

## Optional integrations

Leave optional provider variables unset to run without those providers. Partial or invalid provider configuration can prevent startup.

| Feature | Configuration |
| --- | --- |
| AI summaries, questions and extraction | Set `LLM_API_URL`, `LLM_API_KEY`, `LLM_MODEL`, and `LLM_PROVIDER_LABEL` together. The URL must be an HTTPS Chat Completions endpoint ending in `/chat/completions`. No model is bundled with the API. |
| Personal Gmail | Set `GMAIL_CLIENT_ID`, `GMAIL_CLIENT_SECRET`, and `GMAIL_TOKEN_KEY` (base64-encoded 32-byte encryption key). Requires HTTPS `APP_URL`; register `${APP_URL}/api/v1/gmail/callback`. The connector requests read-only Gmail access. Keep the encryption key stable for stored connections. |
| Document OCR | `OCR_URL` for the English service; `KHMER_OCR_URL` for the Khmer service. These are separate services, not part of the API image. |
| Notification email | SMTP settings are validated by [the mail adapter](platform/notifymail/smtp.go). Enable only when you intend to send notification emails. |
| S3 document originals | Set backend `s3`, `DOCUMENT_S3_BUCKET`, and `DOCUMENT_S3_REGION`; optionally configure `DOCUMENT_S3_ENDPOINT`, `DOCUMENT_S3_PATH_STYLE`, and dedicated access/secret/session credentials. See [storage configuration](platform/documentstorage/config.go) for exact variables and validation. |

S3 uses HTTPS by default; `DOCUMENT_S3_ALLOW_HTTP=true` is an explicit local-development opt-in. The bucket must already exist and be accessible at startup. The adapter does not create buckets or migrate existing originals. Back up document objects and the database together.

Background notification, email, Gmail, AI, and OCR workers run inside the API process. A listening HTTP server alone does not prove external integrations work. Verify each configured provider separately with test data.

## Migrations, builds, and tests

Migrations are embedded from `migrations/` and run explicitly with `go run ./cmd/migrate`. They use a transaction, advisory lock, and stored checksums. Add a new numbered SQL migration rather than modifying an applied file.

```bash
# Formatting check, static analysis, and unit/protocol tests
test -z "$(gofmt -l .)"
go vet ./...
go test ./...

# Build the runtime commands
mkdir -p bin
go build -o bin/api ./cmd/api
go build -o bin/migrate ./cmd/migrate
go build -o bin/bootstrap ./cmd/bootstrap

# Build the container image
docker build -t jokai-business-api:local .
```

The image contains `/app/api`, `/app/migrate`, and `/app/bootstrap`; its default entrypoint is `/app/api`. Supply environment configuration, network connectivity, and a persistent writable document mount when running it. The companion Compose file manages these for local development and retains its existing `ai-business-api:local` image tag.

Some database integration tests skip when their test database variables are absent. To require all database suites, use a **dedicated disposable PostgreSQL server**, with credentials permitted to create and drop databases:

```bash
export INTEGRATION_DISPOSABLE_SERVER='yes'
export INTEGRATION_ADMIN_URL='postgres://postgres:replace-me@127.0.0.1:5432/postgres?sslmode=disable'
go run ./cmd/integrationcheck
unset INTEGRATION_DISPOSABLE_SERVER INTEGRATION_ADMIN_URL
```

The integration runner creates temporary databases, runs the backend tests, rejects skipped required integration suites, and cleans up its databases. Never point it at a shared or production server. GitLab CI defines an ephemeral PostgreSQL service for this gate in [.gitlab-ci.yml](.gitlab-ci.yml).

## Repository layout

- `cmd/`: API, migrations, administrator bootstrap, and verification commands.
- `internal/`: business domains and their tests.
- `platform/`: configuration, authentication, HTTP handlers, and external adapters.
- `migrations/`: embedded database migrations.
- `api/`: canonical OpenAPI contract.
- `tests/api/`: Bruno requests.
- `tests/s3/`: disposable S3 checker image definition.

## Troubleshooting

- **Startup configuration error:** check required environment variables and ensure optional providers are either fully configured or unset.
- **Database unavailable or missing tables:** verify connectivity, then run migrations against the same database as the API.
- **401 after login:** check issuer, API audience, access-token expiry, and API access to the JWKS URL.
- **403 after login:** check the application's user membership/role and the request's `Origin` against `APP_URL`.
- **Document storage unavailable:** check the absolute local path and permissions, or S3 bucket access and trusted TLS configuration.
- **AI/OCR/email unavailable:** check the provider settings and service health; these features require their optional services.

Keep `.env`, `.local/`, private keys, logs, and backups out of Git. The supplied `.gitignore` excludes these local files. Production rollout still requires environment-specific identity/TLS configuration, provider verification, and recovery validation.
