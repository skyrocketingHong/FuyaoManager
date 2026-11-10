# Fuyao 管理前端（本地开发 MVP）

[English](./README.md) | 简体中文

FuyaoHomepage 内容管理系统的 SvelteKit + Svelte 5 + TypeScript 管理台。本包只实现最小可用闭环：首次 Passkey 登记、Passkey 登录/注销、站点列表、站点详情，以及带乐观并发冲突处理的站点配置编辑。刻意不提供文章、模块、密钥与构建等菜单或页面；迭代计划见 [admin/ROADMAP_ZH.md](../ROADMAP_ZH.md)。

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
npm run admin:check          # svelte-check（类型检查）
npm run admin:test           # vitest 单元测试（API client、编辑状态机、i18n、Crossfade 规则）
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
├── app.css                       # 全局设计令牌（日/夜）与基础样式
├── lib/
│   ├── api/
│   │   ├── client.ts             # 同源 fetch 封装：CSRF、If-Match、problem+json、ETag
│   │   ├── management.ts         # 基于生成 schema 的类型化端点
│   │   ├── passkey.ts            # @simplewebauthn/browser 的 WebAuthn 仪式（无类型断言）
│   │   └── schema.d.ts           # 由 contracts/openapi/management.yaml 生成
│   ├── sites/                    # 站点业务模块（不含标记）
│   │   ├── site-detail.svelte.ts # 乱序安全的站点加载
│   │   └── settings-editor.svelte.ts  # 编辑状态机：dirty 字段、412 同步、保存语义
│   ├── i18n/                     # zh-CN / en-US 字典与 Crossfade 切换
│   ├── theme/                    # 日/夜主题状态
│   ├── stores/session.svelte.ts  # 会话状态
│   └── components/
│       ├── ui/                   # Crossfade、Button、Field、StatusMessage（通用 UI）
│       └── sites/                # 绑定业务模块的页面组合
└── routes/                       # 薄路由按 siteId keyed，层级与路径对应
    ├── +page.svelte              # 入口路由（登录或站点）
    ├── login/                    # Passkey 登录
    ├── bootstrap/                # 首次许可登记
    └── sites/
        ├── +page.svelte          # 站点列表（按成员关系）
        └── [siteId]/
            ├── +page.svelte      # 站点详情
            └── settings/+page.svelte # 配置编辑页
```

## 行为说明

- 配置编辑由 `GET .../settings` 的强 ETag 保护：保存发送 `If-Match`，且只提交用户实际修改过的字段，不会覆盖他人对未触碰字段的更新。收到 `412` 时，同步会为未编辑字段采纳服务器新值、保留真实编辑为待保存（并给出明确冲突提示），绝不丢弃未保存输入；保存请求进行中输入的值不会被误标为已保存。viewer 角色看到只读表单。
- 站点页面整个组合按 `siteId` 重建且加载具备乱序防护，跨站点切换（含快速往返、乱序响应）不会展示或写入其他站点的数据。
- 界面文案全部来自 i18n 字典并按项目规范以 Crossfade 切换；日/夜主题经 `data-theme` 自适应。
- Passkey 是唯一登录方式；全栈不存在密码、开发管理员或匿名写接口。
