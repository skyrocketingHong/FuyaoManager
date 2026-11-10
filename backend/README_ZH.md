# Fuyao 管理后端（本地开发 MVP）

[English](./README.md) | 简体中文

FuyaoHomepage 内容管理系统的 Go 后端。本包是敏捷开发的第一个工作包：真实 PostgreSQL 持久化、仅 Passkey 的 WebAuthn 可发现凭据认证，以及“站点列表、站点详情、站点配置保存”的最小业务闭环。管理前端不在本包范围内。

## 状态与边界

本包已验证：

- 真实 PostgreSQL 持久化与 SQL migrations（不以内存或 JSON 代替持久化）。
- 基于 [go-webauthn](https://github.com/go-webauthn/webauthn) 的真实 WebAuthn：注册含证明验证，登录为可发现凭据并严格校验 origin、RP ID 哈希、签名、用户验证（UV）与签名计数。不存在密码、Bearer 或开发管理员登录。
- 首次管理员由本地 CLI bootstrap 生成短时、单次、限尝试次数的登记许可；许可仅授权 Passkey 登记，不是登录凭据。登记在单个事务中完成并以咨询锁串行化：许可有效性（含挑战签发后的过期）、首位管理员唯一性、凭据写入与用户激活要么全部提交要么全部回滚，并发完成只可能有一个成功。
- Cookie 会话支持服务端注销、CSRF 令牌校验，非安全方法严格校验 Origin。
- 站点访问按成员关系隔离（owner/editor/viewer 角色），配置保存采用强 ETag 与 `If-Match` 乐观并发（缺失 428；过期或属于其他站点 412）。ETag 内嵌站点 UUID，其他站点的标签即使修订号相同也无法通过，`*` 通配形式一律拒绝。

明确未实现（也不宣称完成）：Tailscale 私网接入、30 分钟公网门禁、文章/页面/模块、站群分发、密钥与源码构建运维、生产部署。服务启动时拒绝任何非回环地址绑定。

## 环境要求

- Go 1.25 及以上（以 Go 1.27.3 开发并验证）。
- PostgreSQL 14 及以上（以 PostgreSQL 18 验证）。
- 本地管理前端源供 WebAuthn 使用，默认 `http://localhost:5174`，RP ID 为 `localhost`。

## 目录结构

```text
admin/backend/
├── cmd/fuyao-admin/          CLI 入口
├── internal/cli/             migrate / serve / bootstrap-admin / create-site
├── internal/config/          环境变量配置与校验
├── internal/db/              连接池与 SQL migration 执行器
├── internal/model/           共享领域记录
├── internal/store/           PostgreSQL 持久化（按聚合分文件）
├── internal/wa/              WebAuthn RP 配置与适配
├── internal/httpapi/         HTTP 处理器、中间件与集成测试
└── internal/testauth/        仅供测试使用的合成认证器
```

HTTP 契约位于仓库根目录 `contracts/openapi/management.yaml`，只描述已实现的接口。

## 配置

全部配置来自环境变量，模板见 `config.example.env`（不含任何真实凭据）。

| 变量                          | 默认值                  | 用途                                                                                             |
| ----------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------ |
| `FUYAO_ADMIN_DATABASE_URL`    | 必填                    | PostgreSQL DSN。真实值不得提交。                                                                 |
| `FUYAO_ADMIN_ADDR`            | `127.0.0.1:8787`        | 监听地址；必须为回环地址，否则拒绝启动。                                                         |
| `FUYAO_ADMIN_RP_ID`           | `localhost`             | WebAuthn RP ID。                                                                                 |
| `FUYAO_ADMIN_RP_ORIGINS`      | `http://localhost:5174` | 逗号分隔的允许 origin。明文 HTTP 仅允许回环主机；出现 https origin 时启用 Cookie `Secure` 属性。 |
| `FUYAO_ADMIN_RP_DISPLAY_NAME` | `Fuyao Admin`           | 创建选项中的显示名。                                                                             |
| `FUYAO_ADMIN_SESSION_TTL`     | `12h`                   | 会话有效期。                                                                                     |
| `FUYAO_ADMIN_CHALLENGE_TTL`   | `5m`                    | WebAuthn 挑战有效期。                                                                            |
| `FUYAO_ADMIN_PERMIT_TTL`      | `10m`                   | 登记许可有效期。                                                                                 |

## 同源联调拓扑

管理前端开发服务器运行在 `http://localhost:5174`，并将 `/api` 代理到
`127.0.0.1:8787`（见 `admin/frontend/vite.config.ts`）。浏览器只与
`http://localhost:5174` 通信，WebAuthn origin 与会话 Cookie 因此同为第一方且同源。
localhost 页面直连 `http://127.0.0.1:8787` 属于跨站上下文（IP 字面量与主机名永远不同站），
SameSite Cookie 会被丢弃，请勿使用该拓扑。

## 快速开始（本地开发）

```bash
# 在 admin/backend 目录下
export FUYAO_ADMIN_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/fuyao_admin'  # 本地 DSN

go run ./cmd/fuyao-admin migrate
go run ./cmd/fuyao-admin bootstrap-admin --username alice --display-name Alice
# 打印一次性许可，例如 MFRG-GZYT-MZWU-4TCO（10 分钟内有效）
go run ./cmd/fuyao-admin create-site --slug blog --name "Fuyao Blog" --locale zh-CN --owner alice
go run ./cmd/fuyao-admin serve   # http://127.0.0.1:8787
```

首次流程：

1. `bootstrap-admin` 在已存在激活管理员后拒绝运行。许可码只在终端打印一次，是保密信息，不写日志，且仅授权 Passkey 登记。首位管理员登记完成后，其余许可全部失效：options 端点返回 `409 bootstrap_closed`，verify 事务以原子方式执行同一约束。
2. 管理前端（后续工作包）将许可提交 `POST /api/v1/auth/register/options`，执行 `navigator.credentials.create()`，再以 `POST /api/v1/auth/register/verify` 完成登记并直接登录。
3. 后续访问使用 `POST /api/v1/auth/login/options` 与 `POST /api/v1/auth/login/verify`（可发现凭据，强制 UV）。
4. `GET /api/v1/session` 查询登录态；CSRF 令牌在整个会话内稳定，多标签页均可继续写操作。写操作要求 `X-CSRF-Token` 头与允许的 `Origin`。

## HTTP Cookie 说明

- `fuyao_admin_session`（path `/`，HttpOnly，SameSite=Lax）保存不透明令牌；服务端只存其 SHA-256 摘要，注销时会话即失效。SameSite=Lax 之所以可行，是因为前端经开发代理（或部署时的反向代理）同源访问 API，而不是跨站直连。
- `fuyao_admin_auth_tx`（path `/api/v1/auth`，HttpOnly，SameSite=Lax）绑定 WebAuthn ceremony；挑战单次使用并随 cookie 过期。
- 全回环 HTTP 开发配置下，Cookie 有意不携带 `Secure` 属性，否则浏览器会丢弃；只要出现 https（或混合）配置即启用 `Secure`。该例外严格限定于回环 origin，其他 HTTP 主机一律拒绝。

## 测试

集成测试连接真实 PostgreSQL，每个测试创建随机命名的独立 schema（结束后删除）：

```bash
# 在 admin/backend 目录下
FUYAO_ADMIN_TEST_DSN='postgres://user@/fuyao_mvp_test?host=/path/to/socket&port=5432' go test ./...
```

`FUYAO_ADMIN_*_TTL` 存在但解析失败时启动直接报错，不会悄悄回退默认值。

`FUYAO_ADMIN_TEST_DSN` 未设置时，测试会查找仓库本地描述文件 `.fuyao/cms-mvp/postgres-test.json`；两者都不可用时优雅跳过。测试拒绝在 `fuyao_mvp_test` 以外的数据库上运行。测试会话只在 `_test.go` 内构造，运行时代码没有等价后门。

测试覆盖：匿名访问拒绝、bootstrap/注册/登录/配置完整生命周期、并发首次登记（仅一者成功）、挑战签发后许可过期、完整 ETag 的 If-Match 绑定（跨站标签、伪造后缀、通配拒绝、428/412）、两个并发客户端共享稳定 CSRF、verify 路径请求体限额与尾随 JSON 拒绝、CSRF 与 Origin 校验、成员隔离与 viewer 拒写及跨站 404、挑战重放/过期与签名篡改、许可过期与单次使用、TTL 配置错误拒绝、配置字段校验。

## 验证

```bash
cd admin/backend
gofmt -l .
go vet ./...
go test ./...
go build ./...
```
