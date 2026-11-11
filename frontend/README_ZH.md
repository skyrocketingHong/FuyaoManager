# Fuyao 管理前端（本地开发 MVP）

[English](./README.md) | 简体中文

FuyaoHomepage 内容管理系统的 React 18 + TypeScript + Vite + Ant Design 管理台（品牌图标使用 Simple Icons）。覆盖：首次 Passkey 登记、Passkey 登录/注销、站点列表、站点详情、带乐观并发冲突处理的站点配置编辑、逐站主题选择、构建任务与私有预览链接，以及发布/回滚的发布历史。刻意不提供文章、模块、密钥与源码操作等页面；迭代计划见 [admin/ROADMAP_ZH.md](../ROADMAP_ZH.md)。

## 首次安装

管理前端使用独立锁文件，仓库根目录的 `npm ci` 不会安装它。每次检出后执行一次：

```bash
cd admin/frontend
npm ci
```

随后在两个终端分别启动后端与前端：

```bash
# 终端 1：后端监听 127.0.0.1:8787（需要 FUYAO_ADMIN_DATABASE_URL；
# migrate/bootstrap 见 admin/backend/README_ZH.md）
cd admin/backend && go run ./cmd/fuyao-admin serve

# 终端 2：前端运行在 http://localhost:5174（在仓库根目录执行）
npm run admin:dev
```

- 默认地址 `http://localhost:5174`（端口固定、仅回环）。端口被占用时开发
  服务器报 "Port 5174 is already in use" 退出；请先释放端口，不要放任
  origin 漂移。
- 前端提示网络错误说明后端不可达：检查终端 1、`FUYAO_ADMIN_DATABASE_URL`
  以及 `fuyao-admin serve` 是否监听 127.0.0.1:8787。
- 两个终端分别按 Ctrl-C 停止。

## 联调拓扑

开发服务器运行在 `http://localhost:5174`，`/api` 代理到后端 `127.0.0.1:8787`（`vite.config.ts`）。浏览器只与 `http://localhost:5174` 通信，WebAuthn origin 与会话 Cookie 保持同源第一方。不要让 localhost 页面直连 `http://127.0.0.1:8787`：IP 字面量相对 localhost 是跨站上下文，SameSite Cookie 会被丢弃。

## 命令

本包使用独立锁文件，不影响 site 依赖解析（首次安装见上文“首次安装”）。

```bash
# 在仓库根目录执行
npm run admin:dev            # 在 http://localhost:5174 启动开发服务器（端口固定、仅回环）
npm run admin:check          # tsc --noEmit（类型检查）
npm run admin:test           # vitest 单元测试（API client、编辑状态机、主题选择、i18n、挂载视图）
npm run admin:build          # 生产构建到 build/
npm run admin:api:generate   # 由 contracts/openapi/management.yaml 重新生成 src/lib/api/schema.d.ts
npm run admin:api:check      # 已提交的 schema.d.ts 与契约漂移时失败
npm run admin:contract:lint  # OpenAPI 语义校验（redocly）
```

开发端口以 `strictPort` 固定：5174 被占用时启动直接失败，不会漂移到其他
origin 导致 Passkey 与 Cookie 失效。

`src/lib/api/schema.d.ts` 由 [openapi-typescript](https://github.com/openapi-ts/openapi-typescript) 生成并随源码提交；先改契约再重新生成，保证 API 类型不会偏离权威 OpenAPI 文件。

## 目录结构

```text
admin/frontend/src/
├── App.tsx                       # 路由表，层级与路径对应
├── main.tsx                      # ConfigProvider（antd 语言与日/夜算法）与路由
├── app.css                       # 布局壳与 prefers-reduced-motion 覆盖
├── lib/                          # 框架无关业务模块（不含 JSX）
│   ├── api/
│   │   ├── client.ts             # 同源 fetch 封装：CSRF、If-Match、problem+json、ETag
│   │   ├── management.ts         # 基于生成 schema 的类型化端点
│   │   ├── passkey.ts            # @simplewebauthn/browser 的 WebAuthn 仪式（无类型断言）
│   │   └── schema.d.ts           # 由 contracts/openapi/management.yaml 生成
│   ├── sites/                    # 站点加载与配置编辑状态机
│   ├── themes/                   # 主题选择存储（目录、If-Match 保存、412 同步）
│   ├── i18n/                     # zh-CN / en-US 字典与外部存储
│   ├── theme/                    # 日/夜主题状态（antd 算法）
│   └── session/                  # 会话状态
├── components/
│   ├── layout/AppShell.tsx       # 顶栏：语言、日/夜、用户、GitHub 图标（Simple Icons）
│   └── sites/SiteNav.tsx         # 站点子导航
└── pages/                        # 路由页面按 siteId keyed，层级与路径对应
    ├── LoginPage.tsx             # Passkey 登录
    ├── BootstrapPage.tsx         # 首次许可登记
    ├── SitesPage.tsx             # 站点列表（按成员关系）
    └── sites/
        ├── SiteDetailPage.tsx    # 站点概览
        ├── SettingsPage.tsx      # 配置编辑
        ├── ThemePage.tsx         # 主题选择与配置
        ├── BuildsPage.tsx        # 构建任务、轮询、私有预览链接
        └── ReleasesPage.tsx      # 当前发布、历史、发布、回滚
```

## 行为说明

- 配置编辑由 `GET .../settings` 的强 ETag 保护：保存发送 `If-Match`，且只提交用户实际修改过的字段，不会覆盖他人对未触碰字段的更新。收到 `412` 时，同步会为未编辑字段采纳服务器新值、保留真实编辑为待保存（并给出明确冲突提示），绝不丢弃未保存输入；保存请求进行中输入的值不会被误标为已保存。viewer 角色看到只读表单。
- 站点页面整个组合按 `siteId` 重建且加载具备乱序防护，跨站点切换（含快速往返、乱序响应）不会展示或写入其他站点的数据。
- 界面文案全部来自 i18n 字典；日/夜主题切换 antd 算法，`prefers-reduced-motion` 下收敛所有动画与过渡（含 antd 的）。
- Passkey 是唯一登录方式；全栈不存在密码、开发管理员或匿名写接口。
