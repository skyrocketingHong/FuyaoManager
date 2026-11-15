[English](./README.md) | **简体中文**

# FuyaoManager 前端

React、TypeScript 和 Ant Design 管理台提供平台内容、站点工作区、用户、操作日志及程序更新管理。平台和站点共用的内容页面位于 `src/pages/content/`，站点专属页面位于 `src/pages/sites/`，功能专属编辑控件位于 `src/features/`。

在仓库根目录执行：

```bash
npm --prefix frontend ci
npm run frontend:dev
npm run frontend:check
npm --prefix frontend test -- --maxWorkers=2
npm run frontend:build
npm run api:generate
npm run api:check
```

开发服务器将 `/api` 代理到本地后端。环境配置与登录方式见[项目说明](../README_ZH.md)。`src/lib/api/schema.d.ts` 由 `contracts/openapi/openapi.yaml` 生成。
