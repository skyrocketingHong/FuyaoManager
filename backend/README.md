**English** | [简体中文](./README_ZH.md)

# FuyaoManager backend

The Go service owns authentication, content storage, site configuration, builds, previews and publication. `internal/content` provides scoped, ETag-protected file writes; `internal/httpapi` exposes the management contract; `internal/store` owns database access. The CLI entry point is `cmd/fuyao-admin`.

See the [project README](../README.md) for setup, the [configuration guide](../CONFIGURATION.md) for environment variables, and the [content guide](../docs/CONTENT_WORKSPACES.md) for source and media boundaries.

```bash
go -C backend test ./...
go -C backend vet ./...
go -C backend build ./...
```

Database integration tests use the dedicated `fuyao_mvp_test` database. Real-theme tests require `FUYAO_TEST_THEME_ROOT` to point to an installed FuyaoHomepage theme checkout.
