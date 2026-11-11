# 内容管理平台路线图（MVP 迭代台账）

[English](./ROADMAP.md) | 简体中文

内容管理平台的敏捷台账。每个工作包交付可运行、已验证的增量；未实现并验证的能力不列为已完成。2026-11-11 起"后续"部分围绕已批准的主题化与 React 迁移轨道（T1 至 T5）重构；其后的功能包保持原有范围。

## 已完成

1. **后端 MVP（本地开发）**（`admin/backend`）：PostgreSQL + SQL migrations、仅 Passkey 的 WebAuthn 认证（可发现凭据、强制 UV）、CLI（`migrate`、`serve`、`bootstrap-admin`、`create-site`）、带 CSRF/Origin 校验的 Cookie 会话、事务化 ETag 并发的站点列表/详情/配置。仅回环监听。契约：`contracts/openapi/management.yaml`。
2. **加固轮**：事务化首次登记闭环（咨询锁，并发完成仅一者成功）、verify 时点重检许可有效期、完整 ETag 的 If-Match 绑定（拒绝通配）、会话内稳定 CSRF 令牌（多标签页可用）、verify 路径请求体限额与单对象 JSON、TTL 配置错误显式失败、CORS 暴露 ETag。
3. **管理前端 MVP**（`admin/frontend`）：登记、登录/注销、站点列表/详情/配置（dirty 字段提交、412 冲突恢复、viewer 只读）；由契约生成的 API 类型与漂移检查；中英文 Crossfade；日夜主题；同源开发代理拓扑（5174 端口固定、仅回环）。非浏览器的协议联调（后端 serve + 开发代理 + Cookie/Origin 流程）已在本包验证中覆盖；真实平台认证器仪式与视觉验收仍由用户执行。
4. **T1 —— 主题契约 v1**（`contracts/theme/`）：主题清单（`theme.json`）、构建输入快照元数据（`input.json`）与发布元数据（`release.json`）的机器可校验契约，全部为 JSON Schema draft 2020-12，配套公开安全的正负例 fixture，覆盖版本、路径穿越、能力、URL 约定、摘要与未知字段拒绝。可消费的校验与契约同步落地：Go 包 `admin/backend/internal/themecontract`（schema 编译、主题配置交叉校验、清单与输入配对、能力与 URL 约定缺口计算）以及 `fuyao-admin validate-theme` CLI。预留标识：首个主题 `fuyao-homepage`，首个受信任适配器 `node22-sveltekit-static`。契约文档：`contracts/theme/README_ZH.md`。经 `go test ./internal/themecontract/ ./internal/cli/`、`go vet`、`gofmt`、`go build`、prettier 与仓库隐私审计验证。

## 后续（按序，一次一个工作包）

5. 由用户驱动的真实浏览器回归（Touch ID / 硬件钥匙等真实平台认证器）与视觉验收。
6. **T2 —— 首个主题注册**：`site/` 注册为 `fuyao-homepage` 主题。
   - 边界：新增 `site/theme.json` 与 `site/theme.config.schema.json`（内容与契约 fixture 一致）；`site/scripts/prepare-build-inputs.js` 写入 `private/input.json` 并将 `private/public-config.json` 更名为 `private/site-config.json`，计算三项 SHA-256 输入摘要；构建继续只读冻结快照，可写构建资产镜像保持隔离；不改变路由、URL 与视觉。
   - 完成条件：`fuyao-admin validate-theme` 接受站点主题根目录；常规构建产物与改动前逐文件等价（新增元数据除外）；全部站点检查保持通过。
   - 验证：`cd admin/backend && go run ./cmd/fuyao-admin validate-theme --contracts ../../contracts/theme ../../site`；根目录 `npm run check && npm test && npm run build`；与改动前参照构建的产物差异比对。
7. **T3 —— 后端发布管线**：登记、受控构建执行器、预览、校验、原子切换、回滚。
   - 边界：`admin/backend` 增加逐站主题登记（清单校验与按主题配置 schema 校验的配置存储）、基于平台内容的快照准备、限定于枚举适配器的构建执行器（无管理密钥、无数据库访问、无任意命令）、私有预览根目录、按 `release.schema.json` 与各清单 `requiredArtifacts` 的发布校验，以及携带内容、主题、配置版本与任务状态、失败记录的发布记录；每个新增 HTTP 接口在同一次修改中同步 OpenAPI。
   - 完成条件：从管理 API 驱动、基于现有站点内容的端到端本地发布，产物与 T2 直接构建等价；构建失败或不兼容时旧 release 保持生效，并精确报告缺失的能力或 URL 约定项；回滚恢复逐站的早期精确三元组。
   - 验证：面向任务 PostgreSQL 实例的后端集成测试；OpenAPI 漂移与语义检查；发布、不兼容、失败与回滚的证据日志存于 `.fuyao/theme-migration/verification/`。
8. **T4 —— 管理前端迁移到 React + TypeScript + Vite + 官方 Ant Design**。
   - 边界：在 React 上以原生 antd 组件与设计令牌重建六个路由、Passkey 注册与登录、会话处理、站点列表/详情/配置、CSRF 处理、ETag/412 冲突恢复、跨站请求隔离、中英文切换、日夜主题与减少动态效果支持；保留 Go 后端、OpenAPI 契约、框架无关的纯 TS API 客户端（`client.ts`、`management.ts`、`passkey.ts`、生成的 `schema.d.ts`）以及 Vite 同源代理拓扑（5174 端口固定，WebAuthn Origin 不变）。
   - 完成条件：逐路由证明与 Svelte 控制台的功能对等（无功能回退）；类型生成漂移检查、OpenAPI 语义检查、类型检查、测试与构建全部通过；`admin/frontend` 内不再有 Svelte 运行时。
   - 验证：移植并扩展的 vitest 套件；根目录 `npm run admin:check && npm run admin:test && npm run admin:build`。
9. **T5 —— 联调与本地预览交付**。
   - 边界：同时运行 Go 管理服务、React 管理控制台、本地 PostgreSQL 实例与首主题前台或私有预览；向用户提供 URL 与停止方式；日志、凭据与私有数据不进 Git。
   - 完成条件：用户能本地走通完整流程（登录、配置、发布、预览、回滚）；视觉验收与真实平台认证器仪式按约定保留给用户。
10. **文章**：列表、编辑、发布；复用 `site/` 脚本的 Markdown 管道。
11. **页面与版式模块**；按站点划分的内容边界。
12. **站群分发**与成员管理界面。
13. **服务端密钥配置与 GitHub 源码更新**：构建、部署与回滚入口随 T3 落地；本包补齐其余运维入口及任务状态与失败记录。
14. 将 `site/scripts` 其余职责（媒体处理、校验、监听、外部数据缓存、版本管理、迁移、发布）逐项迁入后台；对照验收后停用旧入口。
15. **Tailscale 私网接入**与 30 分钟公网门禁；生产部署路径。

MVP 不做：导入生产数据、迁移真实密钥、拉取生产站部署。
