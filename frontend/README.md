**English** | [简体中文](./README_ZH.md)

# FuyaoManager frontend

The React, TypeScript and Ant Design console manages platform content, site workspaces, users, audit history and application updates. Shared content pages live in `src/pages/content/`; site-specific pages live in `src/pages/sites/`. Feature-specific editor controls live in `src/features/`.

From the repository root:

```bash
npm --prefix frontend ci
npm run frontend:dev
npm run frontend:check
npm --prefix frontend test -- --maxWorkers=2
npm run frontend:build
npm run api:generate
npm run api:check
```

The development server proxies `/api` to the local backend. Setup and authentication are documented in the [project README](../README.md). `src/lib/api/schema.d.ts` is generated from `contracts/openapi/openapi.yaml`.
