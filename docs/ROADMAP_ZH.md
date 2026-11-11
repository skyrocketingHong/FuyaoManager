# 内容管理平台路线图（MVP 迭代台账）

[English](./ROADMAP.md) | 简体中文

内容管理平台的敏捷台账。每个工作包交付可运行、已验证的增量；未实现并验证的能力不列为已完成。2026-11-11 起"后续"部分围绕已批准的主题化与 React 迁移轨道（T1 至 T5）重构；其后的功能包保持原有范围。

## 已完成

1. **后端 MVP（本地开发）**（`admin/backend`）：PostgreSQL + SQL migrations、仅 Passkey 的 WebAuthn 认证（可发现凭据、强制 UV）、CLI（`migrate`、`serve`、`bootstrap-admin`、`create-site`）、带 CSRF/Origin 校验的 Cookie 会话、事务化 ETag 并发的站点列表/详情/配置。仅回环监听。契约：`contracts/openapi/management.yaml`。
2. **加固轮**：事务化首次登记闭环（咨询锁，并发完成仅一者成功）、verify 时点重检许可有效期、完整 ETag 的 If-Match 绑定（拒绝通配）、会话内稳定 CSRF 令牌（多标签页可用）、verify 路径请求体限额与单对象 JSON、TTL 配置错误显式失败、CORS 暴露 ETag。
3. **管理前端 MVP**（`admin/frontend`）：登记、登录/注销、站点列表/详情/配置（dirty 字段提交、412 冲突恢复、viewer 只读）；由契约生成的 API 类型与漂移检查；中英文 Crossfade；日夜主题；同源开发代理拓扑（5174 端口固定、仅回环）。非浏览器的协议联调（后端 serve + 开发代理 + Cookie/Origin 流程）已在本包验证中覆盖；真实平台认证器仪式与视觉验收仍由用户执行。
4. **T1 —— 主题契约 v1**（`contracts/theme/`）：主题清单（`theme.json`）、构建输入快照元数据（`input.json`）与发布元数据（`release.json`）的机器可校验契约，全部为 JSON Schema draft 2020-12，配套公开安全的正负例 fixture，覆盖版本、路径穿越、能力、URL 约定、摘要与未知字段拒绝。可消费的校验与契约同步落地：Go 包 `admin/backend/internal/themecontract`（schema 编译、主题配置交叉校验、清单与输入配对、能力与 URL 约定缺口计算）以及 `fuyao-admin validate-theme` CLI。预留标识：首个主题 `fuyao-homepage`，首个受信任适配器 `node22-sveltekit-static`。契约文档：`contracts/theme/README_ZH.md`。经 `go test ./internal/themecontract/ ./internal/cli/`、`go vet`、`gofmt`、`go build`、prettier 与仓库隐私审计验证。
5. **T2 —— 首个主题注册**（`site/` 注册为 `fuyao-homepage`）：`site/theme.json` 与 `site/theme.config.schema.json` 与契约 fixture 一致；`prepare-build-inputs.js` 写入 `private/input.json`（主题 id/版本与 `version.json` 绑定，不一致即中止构建）并计算三项输入摘要——公开内容树附加内容路径/媒体映射、站点配置、主题配置；`public-config.json` 更名 `site-config.json` 覆盖全部真实消费方（`vite.config.ts`、build-context、`generate-blog-index.js`、`prepare-content-release.js`）。`--frozen=<snapshot>`（或 `FUYAO_FROZEN_INPUT`）为 T3 提供最小消费入口：冻结快照只读校验（身份、协议、摘要冲突一律拒绝），仅其可写构建资产镜像进入 SvelteKit。共享逻辑位于 `site/scripts/lib/theme-input.js`。经 `fuyao-admin validate-theme` 校验 `site/`、svelte-check、99 项站点测试（含 4 项新增 theme-input 测试）、lint、完整构建与改动前基线的归一化产物对比验证：250/250 个文件、99 个非哈希路径全部一致，其余差异仅为 `__BUILD_TIME__`/`__sveltekit_` 派生的资产哈希重命名与构建时刻字段。
6. **T3-a —— 后端主题登记与逐站选择**（`admin/backend`）：migration 0003 新增 `themes` 目录表与 `site_theme_selections`；操作员驱动的 `fuyao-admin register-theme` CLI 是唯一登记入口（HTTP 接口不接受路径、命令或 Git 地址），登记前按 `contracts/theme` 校验主题根、强制受信任适配器列表、将 source 解析为被校验根目录的绝对真实路径，并存储清单、SHA-256 摘要与配置 schema；重复登记只有在完整身份（摘要与 schema，含声明/未声明之分）一致时幂等。HTTP 面：`GET /api/v1/themes` 与 `GET/PUT /api/v1/sites/{siteId}/theme`，沿用既有成员权限、CSRF、Origin、If-Match 模型，修订号计数独立（`-t` 标签，`t0` 表示未选择）；`themeConfig` 省略按 `{}` 处理，显式 `null` 拒绝；非法配置返回不含路径的 422；未设置 `FUYAO_ADMIN_THEME_CONTRACTS` 时保存返回 503 而非跳过校验。OpenAPI 同步更新，类型生成、漂移与语义检查全绿。面向任务 PostgreSQL 18 实例验证：完整 `go test ./...` 加 gofmt/vet/build，覆盖目录读取、持久化、权限、过期 ETag、并发首次保存与登记冲突回归；证据存于 `.fuyao/theme-migration/verification/t3a/`。

7. **T3-b1 —— 真实快照构建与私有预览闭环**（`admin/backend` + `site/`）：migration 0004 持久化按站点构建任务，创建事务冻结确切版本视图（站点设置快照与修订、主题选择修订、主题身份与配置）；执行器角色为集群级 advisory lock——仅持有者把中断的 `running` 行标记失败并重新入队 `queued`，且持有角色直到 worker 排空，第二实例不会误杀第一实例的活跃任务。执行器所有阶段绑定注册主题根，执行前按登记身份复核磁盘 `theme.json` 与配置 schema 摘要（发现注册后被篡改的源码），构建前用受信任服务端站点需求检查主题 capabilities/URL conventions（缺失项明确失败），以主题自身 Node 流程准备快照并注入数据库保存的站点设置（`site.name/description/defaultLocale`，description 经 `site.site.description` 进入公开 SEO 元数据）与主题配置（JSON 文件），随后以**复制+校验+冻结**把快照与产物固化到任务独立目录——共享主题工作区（`build-inputs/current`、`build/`、`build-context.json`）保持原状恢复。命令在净化环境与独立进程组中运行（取消时组级终止），不继承数据库连接串或管理密钥；这是环境与进程卫生，不是 OS 级沙箱。预览：每个成功构建获得独立回环 origin（按构建监听），入口一次性令牌换取 HttpOnly cookie——根绝对资源、深层路由、声明的 SPA fallback 均保持生产行为，不同构建/站点绝不串产物，origin 与管理 API 不同源，主题脚本无法读取管理响应。HTTP 面：`POST/GET /api/v1/sites/{siteId}/builds`、`GET .../builds/{buildId}`、`POST .../builds/{buildId}/preview-links`，沿用既有成员/CSRF/Origin 模型；OpenAPI 与生成类型同次更新。已在任务 PostgreSQL 实例验证：闭环测试经 API 创建两个真实任务并真实执行 Node/SvelteKit 构建后走预览 HTTP 面（数据库设置冻结进快照、摘要入库、产物只读、第二构建 origin 只服务自身内容、跨 origin 令牌与匿名访问被拒），另含能力缺口构建的持久化明确失败；后端全量、site 受影响配置/主题输入测试、OpenAPI 漂移检查与 prettier 通过。证据存于 `.fuyao/theme-migration/verification/t3b1/`。

## 后续（按序，一次一个工作包）

7. 由用户驱动的真实浏览器回归（Touch ID / 硬件钥匙等真实平台认证器）与视觉验收。
8. **T3 —— 后端发布管线**：受控构建执行器、预览、校验、原子切换、回滚（登记与逐站选择已作为 T3-a 落地；快照构建与私有预览已作为上方 T3-b1 落地——本项剩余范围为发布：按 `release.schema.json` 与 `requiredArtifacts` 校验、原子切换、回滚、发布记录）。
   - 边界：`admin/backend` 增加基于平台内容、消费 T3-a 选择记录与受信任清单数据的快照准备、限定于枚举适配器的构建执行器（无管理密钥、无数据库访问、无任意命令）、私有预览根目录、按 `release.schema.json` 与各清单 `requiredArtifacts` 及阻止发布的能力/URL 约定缺口检查的发布校验，以及携带内容、主题、配置版本与任务状态、失败记录的发布记录；每个新增 HTTP 接口在同一次修改中同步 OpenAPI。
   - 完成条件：从管理 API 驱动、基于现有站点内容的端到端本地发布，产物与 T2 直接构建等价；构建失败或不兼容时旧 release 保持生效，并精确报告缺失的能力或 URL 约定项；回滚恢复逐站的早期精确三元组。
   - 验证：面向任务 PostgreSQL 实例的后端集成测试；OpenAPI 漂移与语义检查；发布、不兼容、失败与回滚的证据日志存于 `.fuyao/theme-migration/verification/`。
9. **T4 —— 管理前端迁移到 React + TypeScript + Vite + 官方 Ant Design**。
   - 边界：在 React 上以原生 antd 组件与设计令牌重建六个路由、Passkey 注册与登录、会话处理、站点列表/详情/配置、CSRF 处理、ETag/412 冲突恢复、跨站请求隔离、中英文切换、日夜主题与减少动态效果支持，品牌图标统一使用 Simple Icons（保留必要可访问名称，不为凑图标添加无用文案或装饰）；管理端技术栈为 TypeScript + React + Ant Design + Simple Icons，仅限前端——Go 服务端保持 Go（不做服务端 TypeScript 迁移），公开前台保持 Svelte 作为首个主题。保留 OpenAPI 契约、框架无关的纯 TS API 客户端（`client.ts`、`management.ts`、`passkey.ts`、生成的 `schema.d.ts`）以及 Vite 同源代理拓扑（5174 端口固定，WebAuthn Origin 不变）。
   - 完成条件：逐路由证明与 Svelte 控制台的功能对等（无功能回退）；类型生成漂移检查、OpenAPI 语义检查、类型检查、测试与构建全部通过；`admin/frontend` 内不再有 Svelte 运行时。
   - 验证：移植并扩展的 vitest 套件；根目录 `npm run admin:check && npm run admin:test && npm run admin:build`。
10. **T5 —— 联调与本地预览交付**。
    - 边界：同时运行 Go 管理服务、React 管理控制台、本地 PostgreSQL 实例与首主题前台或私有预览；向用户提供 URL 与停止方式；日志、凭据与私有数据不进 Git。
    - 完成条件：用户能本地走通完整流程（登录、配置、发布、预览、回滚）；视觉验收与真实平台认证器仪式按约定保留给用户。
11. **文章**：列表、编辑、发布；复用 `site/` 脚本的 Markdown 管道。
12. **页面与版式模块**；按站点划分的内容边界。
13. **站群分发**与成员管理界面。
14. **服务端密钥配置与 GitHub 源码更新**：构建、部署与回滚入口随 T3 落地；本包补齐其余运维入口及任务状态与失败记录。
15. 将 `site/scripts` 其余职责（媒体处理、校验、监听、外部数据缓存、版本管理、迁移、发布）逐项迁入后台；对照验收后停用旧入口。
16. **Tailscale 私网接入**与 30 分钟公网门禁；生产部署路径。

MVP 不做：导入生产数据、迁移真实密钥、拉取生产站部署。
