# Repository Guidelines

## Project Structure & Module Organization

This is a Go module for an IMAP-to-HTTP mail ingress service.

- `main.go` wires config loading, signal handling, listeners, admin API, and webhook delivery.
- `config/` loads YAML config and stores the starter config template.
- `mailer/` owns IMAP login, IDLE listening, message parsing, and `Seen` acknowledgement.
- `webhook/` builds and sends outbound webhook payloads.
- `state/` stores JSON checkpoint and retry cooldown state.
- `admin/` serves optional read-only local endpoints and embedded OpenAPI JSON.
- `api/openapi.yaml` is the OpenAPI source; `admin/openapi.json` is generated.
- `cmd/genopenapi/` contains the OpenAPI YAML-to-JSON generator.
- `docs/` contains design documentation such as processing modes.

Do not commit local secrets: `.env` and `config.yaml` are intentionally ignored.

## Build, Test, and Development Commands

Use `GOWORK=off` when working from a parent workspace:

```bash
GOWORK=off go test ./...
GOWORK=off go build ./...
GOWORK=off go generate ./admin
```

- `go test ./...` verifies all packages compile and tests pass.
- `go build ./...` builds every package.
- `go generate ./admin` regenerates `admin/openapi.json` from `api/openapi.yaml`.
- `go build -ldflags="-s -w -X main.version=vX.Y.Z" -o mail-forwarder .` builds a release-style binary.

Run locally with:

```bash
mail-forwarder init-config
mail-forwarder -config config.yaml
```

## Coding Style & Naming Conventions

Use standard Go formatting:

```bash
gofmt -w .
```

Keep package names short and lowercase (`mailer`, `webhook`, `state`). Prefer explicit config fields and clear error messages over hidden behavior. Avoid adding business routing logic to `mailer`; keep protocol conversion separate from downstream workflow logic.

## Testing Guidelines

Tests use Go’s standard `testing` package. Add focused `*_test.go` files beside the package under test. Prioritize tests for state transitions, retry backoff, config validation, and OpenAPI generation. Always run:

```bash
GOWORK=off go test ./...
```

## Commit & Pull Request Guidelines

History uses short imperative or descriptive commits, for example:

- `Add processing modes and retry state`
- `Use project name for Aliyun image`
- `Fix IMAP event handling`

Keep PRs scoped. Include a summary, config or API changes, test results, and release impact. If `api/openapi.yaml` changes, regenerate and include `admin/openapi.json`.

## Security & Configuration Tips

Treat IMAP passwords, webhook secrets, `.env`, `config.yaml`, and state files as sensitive. Starter configs should use placeholders and `dry_run: true`. The admin API is disabled by default; bind to `127.0.0.1:6245` unless external access is intentional.
