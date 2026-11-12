# Fuyao 管理后端

[English](./README.md) | 简体中文

FuyaoManager 内容管理系统的 Go 后端：真实 PostgreSQL 持久化、CLI 一次性码认证、网页建站与目录供应、主题登记与选择、快照构建与私有预览、发布与回滚管线。React 管理台（`frontend`）与该 API 同源通信。

## 状态与边界

本包已验证：

- 真实 PostgreSQL 持久化与 SQL migrations（不以内存或 JSON 代替持久化）。
- **一次性码认证为默认登录方式**：每次登录都使用服务器 CLI 打印的一次性码（`fuyao-admin init-admin` 用于首个管理员，`fuyao-admin login-code --username` 用于后续每次登录）。登录码含 80 位熵，服务端只存 SHA-256 摘要，短时有效（`FUYAO_ADMIN_AUTH_CODE_TTL`），每次呈现消耗一次尝试（6 次后锁定），由 `POST /api/v1/auth/exchange` 在单个事务中兑换——码消费、用途校验（初始化与登录分离）、首位管理员激活（登记咨询锁内）与会话创建要么全部提交要么全部回滚，同一码的并发兑换只可能有一个成功。端点另有按地址的内存限速。每次兑换签发全新会话（轮换）。
- Passkey（WebAuthn）数据与端点为本地开发原样保留，适用条件见下文“Passkey 兼容性”；HTTP 登录从不依赖它们。
- Cookie 会话支持服务端注销、CSRF 令牌校验，非安全方法严格校验 Origin；明文 HTTP origin 仅允许回环或 `FUYAO_ADMIN_TRUSTED_HOSTS` 明确列出的主机。
- 站点访问按成员关系隔离（owner/editor/viewer 角色），配置保存采用强 ETag 与 `If-Match` 乐观并发（缺失 428；过期或属于其他站点 412）。
- 网页建站（`POST /api/v1/sites`）：slug 校验、可选初始主题选择（与主题 PUT 同标准校验）、可见性默认私有、明确的供应状态——`FUYAO_ADMIN_SITES_ROOT` 下的按站点内容/配置目录树必须真实创建后站点才报告 `ready`；失败可通过 `POST /api/v1/sites/{siteId}/provision` 幂等重试。
- 主题登记、快照构建、私有预览、发布与回滚按 T3-a/b1/b2 工作包交付（见 `admin/ROADMAP.md`）。
- 可选托管构建后的管理台静态产物（`FUYAO_ADMIN_UI_DIR`），带受限 CSP 与 SPA fallback。
- **唯一内容权威源**（`internal/content` + `/api/v1/sites/{siteId}/...`）：Markdown 文章 CRUD（frontmatter 字段与主题索引生成器消费的完全一致、`draft` 即公开输出门控、基于摘要的 If-Match、`%2F` 编码的 category/slug 路径）、友链文档、社交链接、可视化 site.yaml 配置（资料/仓库/SEO/服务，AMap 安全码只写不读）。生产模式构建消费按站点内容/配置/favicon 根目录——冻结快照反映 API 编辑，草稿绝不进入快照。
- **相册与足迹**：照片上传（按字节嗅探类型、40 MiB 上限、服务端受控 `yyyy/mm/dd` 路径）经站点主题自身索引器异步处理 EXIF/缩略图（`site_media_tasks` 持久化、带失败记录与重启恢复），按年相册元数据编辑（标题/说明/替代文本/排序，按 filename 配对），引用检查式删除（仍被生效或可回滚发布冻结元数据引用的照片以 `photo_in_use` 拒绝），以及足迹点位文档（双语名称、GCJ-02 边界、city/spot 类型、YYYYMMDD 日期、唯一 id）支撑控制台的地图选点。

## 环境要求

- Go 1.25 及以上（以 Go 1.27.3 开发并验证）。
- PostgreSQL 14 及以上（以 PostgreSQL 18 验证）。
- 主题构建需要 Node 22（由构建执行器调用；见 `FUYAO_ADMIN_REPO_ROOT`）。

## 目录结构

```text
backend/
├── cmd/fuyao-admin/          CLI 入口
├── internal/cli/             migrate / serve / init-admin / login-code / revoke-sessions / bootstrap-admin / create-site / validate-theme / register-theme
├── internal/config/          环境变量配置与校验
├── internal/db/              连接池与 SQL migration 执行器
├── internal/model/           共享领域记录
├── internal/siteprovision/   按站点内容/配置目录供应
├── internal/store/           PostgreSQL 持久化（按聚合分文件）
├── internal/themecontract/   contracts/theme 契约 schema 加载与校验
├── internal/themeregistry/   受信任主题根目录加载入目录（仅 CLI 驱动）
├── internal/wa/              WebAuthn RP 配置与适配
├── internal/httpapi/         HTTP 处理器、中间件、UI 托管与集成测试
└── internal/testauth/        仅供测试使用的合成认证器
```

HTTP 契约位于仓库根目录 `contracts/openapi/management.yaml`，只描述已实现的接口。

## 配置

全部配置来自环境变量，模板见 `config.example.env`（不含任何真实凭据）。

| 变量                                          | 默认值                                      | 用途                                                                                                                                                                         |
| --------------------------------------------- | ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_DATABASE_URL`                    | 必填                                        | PostgreSQL DSN。真实值不得提交。                                                                                                                                             |
| `FUYAO_ADMIN_ADDR`                            | `127.0.0.1:8787`                            | 监听地址；回环地址始终可用，其他主机必须列入 `FUYAO_ADMIN_TRUSTED_HOSTS`。                                                                                                   |
| `FUYAO_ADMIN_TRUSTED_HOSTS`                   | 未设置                                      | 逗号分隔的主机列表，允许非回环绑定与明文 HTTP origin（例如服务器 Tailscale IP）。                                                                                            |
| `FUYAO_ADMIN_RP_ID`                           | `localhost`                                 | WebAuthn RP ID（仅 Passkey 兼容路径）。                                                                                                                                      |
| `FUYAO_ADMIN_RP_ORIGINS`                      | `http://localhost:5174`                     | 逗号分隔的允许 origin（驱动 CORS、Origin 防护与 Cookie `Secure`）。明文 HTTP 仅允许回环或受信主机。                                                                          |
| `FUYAO_ADMIN_RP_DISPLAY_NAME`                 | `Fuyao Admin`                               | 创建选项中的显示名。                                                                                                                                                         |
| `FUYAO_ADMIN_SESSION_TTL`                     | `12h`                                       | 会话有效期。                                                                                                                                                                 |
| `FUYAO_ADMIN_CHALLENGE_TTL`                   | `5m`                                        | WebAuthn 挑战有效期。                                                                                                                                                        |
| `FUYAO_ADMIN_PERMIT_TTL`                      | `10m`                                       | Passkey 登记许可有效期（遗留路径）。                                                                                                                                         |
| `FUYAO_ADMIN_AUTH_CODE_TTL`                   | `10m`                                       | 一次性登录码有效期（`init-admin`、`login-code`）。                                                                                                                           |
| `FUYAO_ADMIN_THEME_CONTRACTS`                 | 未设置                                      | 仓库内 `contracts/theme` 路径。设置后启动时编译契约 schema，主题配置保存按各主题自身 schema 校验；未设置时保存主题选择返回 `503 theme_contracts_unavailable`，绝不跳过校验。 |
| `FUYAO_ADMIN_REPO_ROOT`                       | 工作目录                                    | 仓库根目录，锚定 `<repo>/site/.fuyao` 下的构建工作区与发布根目录。从其他目录启动 `serve` 时应显式设置。                                                                      |
| `FUYAO_ADMIN_SITES_ROOT`                      | `<repo>/site/.fuyao/admin-sites`            | 按站点内容/配置树根目录（`<root>/<siteId>/{content,config}`）。                                                                                                              |
| `FUYAO_ADMIN_UI_DIR`                          | 未设置                                      | 构建后的管理台目录；设置后非 `/api` 路径托管该静态 SPA（生产拓扑）。                                                                                                         |
| `FUYAO_ADMIN_PREVIEW_BIND`                    | `127.0.0.1`                                 | 预览监听绑定地址；设为私网地址即支持远程（Tailscale）预览。                                                                                                                  |
| `FUYAO_ADMIN_PREVIEW_EXTERNAL_BASE`           | 未设置                                      | 预览链接返回给浏览器的 URL 基址（如 `http://100.117.154.115`）；存在远程客户端时绝不使用回环字面量。                                                                         |
| `FUYAO_ADMIN_DEPLOYMENT_ENABLED`              | `false`                                     | 发布开关。关闭时为运行隔离：建站、内容编辑、真实构建与私有预览可用，但不启动发布服务器、不 Reconcile 或应用 Caddy，发布/回滚/可见性变更/edge 重应用返回 `503 deployment_disabled`；历史发布数据仍可读。 |
| `FUYAO_ADMIN_EDGE_SNIPPET_DIR`                | 未设置                                      | 受管 Caddy 片段目录；设置即启用已发布站点边缘管理。发布开关关闭时片段管理不生效。                                                                                             |
| `FUYAO_ADMIN_EDGE_APPLY_CMD`                  | `sudo -n /usr/local/sbin/fuyao-caddy-apply` | 运维控制的校验+reload 命令，参数固定。                                                                                                                                       |
| `FUYAO_ADMIN_EDGE_PRIVATE_BIND`               | `127.0.0.1`                                 | 私有站点入口的绑定与访问地址（部署时为 Tailscale 地址）。                                                                                                                    |
| `FUYAO_ADMIN_EDGE_PUBLIC_BIND`                | `:`（全部接口）                             | 公开站点入口的绑定地址。                                                                                                                                                     |
| `FUYAO_ADMIN_EDGE_PUBLIC_HOST`                | 未设置                                      | 公开入口对外通告的 URL 主机（全网绑定时填公网 IP）。                                                                                                                         |
| `FUYAO_ADMIN_BUILDS_ROOT`                     | `<repo>/site/.fuyao/admin-builds`           | 持久构建任务根目录；部署时应指向程序目录之外。                                                                                                                               |
| `FUYAO_ADMIN_RELEASES_ROOT`                   | `<repo>/site/.fuyao/admin-releases`         | 持久发布根目录；部署时应指向程序目录之外。                                                                                                                                   |
| `FUYAO_ADMIN_PORT_MIN`/`FUYAO_ADMIN_PORT_MAX` | `20000`/`20100`                             | 运维批准的端口范围（含端点），用于站点入口与预览监听；非法或倒置的取值直接拒绝启动。                                                                                         |
| `FUYAO_ADMIN_BUILD_MODE`                      | `ci`                                        | 平台构建的内容来源：`ci` 使用仓库 fixture 内容；`production` 需要受信任的内容根目录设置。                                                                                    |
| `FUYAO_ADMIN_REQUIRE_CAPABILITIES`            | `blog`                                      | 受信任站点需求：主题构建与发布前必须满足的能力。默认值仅保证博客能力兼容；实际启用首主题全部页面的站点必须设置为 `blog,albums,footprint,friends,pay,favicon`。               |
| `FUYAO_ADMIN_REQUIRE_URL_CONVENTIONS`         | `posts,blog-rss,sitemap`                    | 同上，针对 URL 约定；首主题全部功能还需 `release.json,data,media-albums,blog-search`。                                                                                       |

## 同源联调拓扑

管理前端开发服务器运行在 `http://localhost:5174`，并将 `/api` 代理到
`127.0.0.1:8787`（见 `frontend/vite.config.ts`）。浏览器只与
`http://localhost:5174` 通信，会话 Cookie 因此同为第一方且同源。
localhost 页面直连 `http://127.0.0.1:8787` 属于跨站上下文（IP 字面量与主机名永远不同站），
SameSite Cookie 会被丢弃，请勿使用该拓扑。生产环境由 Go 服务直接托管构建后的管理台
（`FUYAO_ADMIN_UI_DIR`），天然同源。

## 快速开始（本地开发）

```bash
# 在 backend 目录下
export FUYAO_ADMIN_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/fuyao_admin'  # 本地 DSN

go run ./cmd/fuyao-admin migrate

# 首个管理员：CLI 授权绑定 + 一次性初始化码
go run ./cmd/fuyao-admin init-admin --username alice --display-name Alice
# 打印一次性初始化码，例如 MFRG-GZYT-MZWU-4TCO（10 分钟内有效）

# 后续每次登录：新签一次性登录码
go run ./cmd/fuyao-admin login-code --username alice

# 可选：从 CLI 建站（管理台网页也可建站）
go run ./cmd/fuyao-admin create-site --slug blog --name "Fuyao Blog" --locale zh-CN --owner alice

# 将主题登记入目录（操作员驱动；HTTP 接口不接受文件路径）。
# 记录的 source 即主题根目录本身，登记前完成清单与配置 schema 全量校验：
go run ./cmd/fuyao-admin register-theme --contracts ../../contracts/theme ../../site

FUYAO_ADMIN_THEME_CONTRACTS="$PWD/../../contracts/theme" \
  go run ./cmd/fuyao-admin serve   # http://127.0.0.1:8787
```

## 认证流程

1. **平台未初始化**：匿名浏览器从 `GET /api/v1/session` 得到 `platformInitialized: false`，登录页展示准确的服务器 CLI 操作指引而非空表单。`fuyao-admin init-admin --username <name>` 注册仍为 pending 的首个管理员并打印单次初始化码；第一个匿名访问者绝无可能直接取得管理员身份。
2. **首次登录**：管理台将该码提交 `POST /api/v1/auth/exchange`。兑换事务在首位管理员咨询锁内激活 pending 用户（已存在任何激活管理员则拒绝）并建立会话。初始化码在已有管理员后不能用于任何其他用途。
3. **后续每次登录**：`fuyao-admin login-code --username <name>` 打印新的一次性码，管理台以同样方式兑换。登录码绝不复用：已用码返回 `409 code_used`，过期码 `409 code_expired`，与账号状态不符 `409 code_purpose_mismatch`。
4. **建站**：登录后的所有者直接在网页创建首个站点（基础信息、主题选择、可见性默认私有）；响应报告供应状态，失败的初始化可重试。
5. **注销**：管理台调用 `POST /api/v1/auth/logout`，会话在服务端注销。`fuyao-admin revoke-sessions --username <name>` 从 CLI 注销该用户的全部会话。
6. `GET /api/v1/session` 报告登录态与平台初始化标志；CSRF 令牌在整个会话内稳定，多标签页均可继续写操作。写操作要求 `X-CSRF-Token` 头与允许的 `Origin`。

### Passkey 兼容性

Passkey 端点（`/api/v1/auth/register/*`、`/api/v1/auth/login/*`）与已存凭据保持可用，但仅当 WebAuthn origin 与 `FUYAO_ADMIN_RP_ID`/`FUYAO_ADMIN_RP_ORIGINS` 匹配时才能工作——实际即 localhost 开发拓扑或 https 部署。它们从不干预或阻塞登录码流程，任何登录码路径都不依赖它们。React 管理台不再提供 Passkey 入口；`bootstrap-admin` 保留用于本地 Passkey 开发。

## HTTP Cookie 说明

- `fuyao_admin_session`（path `/`，HttpOnly，SameSite=Lax）保存不透明令牌；服务端只存其 SHA-256 摘要，注销时会话即失效。SameSite=Lax 之所以可行，是因为前端经开发代理、托管产物或反向代理同源访问 API，从不跨站直连。
- `fuyao_admin_auth_tx`（path `/api/v1/auth`，HttpOnly，SameSite=Lax）绑定 WebAuthn ceremony；挑战单次使用并随 cookie 过期。
- 全回环 HTTP 开发配置下，Cookie 有意不携带 `Secure` 属性，否则浏览器会丢弃；只要出现 https（或混合）配置即启用 `Secure`。回环之外的明文 HTTP 仅对 `FUYAO_ADMIN_TRUSTED_HOSTS` 明确列出的主机开放，绝不从请求头猜测。

## 测试

集成测试连接真实 PostgreSQL，每个测试创建随机命名的独立 schema（结束后删除）：

```bash
# 在 backend 目录下
FUYAO_ADMIN_TEST_DSN='postgres://user@/fuyao_mvp_test?host=/path/to/socket&port=5432' go test ./...
```

`FUYAO_ADMIN_*_TTL` 存在但解析失败时启动直接报错，不会悄悄回退默认值。

`FUYAO_ADMIN_TEST_DSN` 未设置时，测试会查找仓库本地描述文件 `.fuyao/cms-mvp/postgres-test.json`；两者都不可用时优雅跳过。测试拒绝在 `fuyao_mvp_test` 以外的数据库上运行。测试会话只在 `_test.go` 内构造，运行时代码没有等价后门。

测试覆盖：匿名访问拒绝、一次性码完整生命周期（初始化探测、初始化码兑换激活 pending 管理员、每次登录码、错误/已用/过期码、用途分离、并发兑换仅一者成功、重放问题码稳定、注销失效）、网页建站（磁盘供应、重复 slug 409、slug 校验、成员隔离、幂等供应重试）、以及 `admin/ROADMAP.md` 记录的配置/主题/构建/发布面，和加固集：完整 ETag 的 If-Match 绑定、两个并发客户端共享稳定 CSRF、verify 路径请求体限额、CSRF 与 Origin 校验、viewer 拒写与跨站 404、TTL 配置错误拒绝，以及 `internal/themeregistry` 中受信任适配器/清单/schema/路径拒绝。

## 验证

```bash
cd backend
gofmt -l .
go vet ./...
go test ./...
go build ./...
```
