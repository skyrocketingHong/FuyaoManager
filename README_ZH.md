[English](./README.md) | **简体中文**

# FuyaoManager

<div align="center">

[![License](https://img.shields.io/badge/license-AGPL--3.0--only-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8.svg)](https://go.dev/)
[![React](https://img.shields.io/badge/React-18-61DAFB.svg)](https://react.dev/)
[![Ant Design](https://img.shields.io/badge/Ant_Design-6-1677ff.svg)](https://ant.design/)

</div>

FuyaoManager 是自托管的站点与内容管理平台：管理站点、内容、媒体任务、主题登记、冻结快照构建、私有预览、发布、回滚，以及受管的反向代理边缘。主题是通过版本化契约接入的外部项目——第一个主题是
[FuyaoHomepage](https://github.com/skyrocketingHong/FuyaoHomepage)（主题 ID `fuyao-homepage`，适配器 `node22-sveltekit-static`）。

本仓库于 2026-11-12 从 FuyaoHomepage 拆分而来，保留了平台开发历史；见
[docs/SOURCE_MIGRATION.md](./docs/SOURCE_MIGRATION.md)（英文）。

## 特性

- **通行密钥认证**：日常登录使用 discoverable 通行密钥（WebAuthn，要求用户验证）；Cookie 会话配合 CSRF 与 Origin 校验。首次运行由 `fuyao-admin init-admin` 签发一次性码，浏览器兑换后绑定通行密钥完成激活——账号在当前 RP 下无凭据时，兑换只获得短有效期受限登记会话，完成绑定仪式后才放开业务。`fuyao-admin login-code` 保留为运维批准的恢复入口。
- **网页建站**：逐站内容、配置与 favicon 目录落盘，供应状态显式记录，支持幂等重试。
- **内容权威源**：文章（frontmatter 感知、草稿门控）、友链、社交链接、可视化站点配置（高德安全码只写不读）、足迹点位——全部写入经由同一个受限内容库，带摘要 ETag 与原子写。
- **相册与媒体任务**：服务端可控路径的上传、所选主题自己的 Node 索引器只读执行、年文档按 filename 编辑、删除受发布引用保护。
- **主题登记**：由运维经 `fuyao-admin register-theme` 登记，theme.json 与配置 schema 依权威契约校验；逐站选择配合 If-Match。
- **构建**：由主题自身准备冻结输入快照，构建运行在任务私有源码副本中（绝不写注册源），记录逐文件源码清单，校验能力与 URL 约定。
- **私有预览**：逐构建的环回 origin，一次性入口令牌；关闭预览撤销令牌、停止监听并释放端口。
- **发布与回滚**：契约校验的 release 元数据、密封只读产物、逐站发布锁内原子切换 live 指针、失败可重试、回滚不重新构建。
- **受管边缘**：由数据库状态渲染 Caddy 片段，逐站公开/私有与显式接口绑定，经固定运维命令应用并支持最后良好状态回退。
- **管理台**：React 18 + 官方 Ant Design 6，中英文、日夜主题、桌面应用式分组外壳（侧栏作用域、顶栏页面插槽）。

## 技术栈

| 层     | 选型                                                     |
| ------ | -------------------------------------------------------- |
| 后端   | Go 1.27、PostgreSQL 18（pgx）、JSON Schema draft 2020-12 |
| 管理台 | TypeScript、React 18、Vite、Ant Design 6、Simple Icons   |
| 契约   | OpenAPI 3.0（管理 API）、JSON Schema（主题契约）         |
| 边缘   | Caddy（受管片段，仅渲染——绝不执行用户输入）              |

## 环境要求

- Go 1.27+
- Node.js 22+（管理台构建）
- PostgreSQL 18（本地或远程；迁移由 CLI 应用）
- 一个待登记的主题项目（如已安装依赖的 FuyaoHomepage 检出）——平台自身在没有主题时正常启动并显示空主题目录。

## 快速开始

```bash
# 管理台（frontend/）
cd frontend && npm ci
npm run api:generate   # 由 ../contracts/openapi/management.yaml 生成类型
npm run build

# 后端（backend/）
cd ../backend
export FUYAO_ADMIN_DATABASE_URL='postgres://fuyao:...@127.0.0.1:5432/fuyao_admin?sslmode=disable'
go run ./cmd/fuyao-admin migrate
go run ./cmd/fuyao-admin init-admin --username admin --display-name 管理员   # 打印一次性码
go run ./cmd/fuyao-admin serve
# 管理台开发服务器：cd frontend && npm run dev（/api 代理到 127.0.0.1:8787）

# 登记主题（运维路径；主题根必须通过契约校验）
go run ./cmd/fuyao-admin register-theme \
  --db "$FUYAO_ADMIN_DATABASE_URL" \
  --contracts ../contracts/theme \
  --root /path/to/FuyaoHomepage/site
```

在管理台兑换一次性码并绑定通行密钥后创建站点、选择主题、写入内容、构建、预览、发布。

## 命令索引

根 `package.json` 转发到独立锁文件的前端包：

| 命令                                              | 用途                                     |
| ------------------------------------------------- | ---------------------------------------- |
| `npm run frontend:dev`                            | Vite 开发服务器（端口 5174，代理到 API） |
| `npm run frontend:build`                          | 类型检查 + 生产构建                      |
| `npm run frontend:check` / `frontend:test`        | 类型检查 / vitest                        |
| `npm run api:generate` / `api:check` / `api:lint` | OpenAPI 类型、漂移检查、redocly lint     |

后端（在 `backend/` 内执行）：

| 命令                          | 用途                                                       |
| ----------------------------- | ---------------------------------------------------------- |
| `fuyao-admin migrate`         | 应用全部 SQL 迁移                                          |
| `fuyao-admin serve`           | 启动管理 API（设置 `FUYAO_ADMIN_UI_DIR` 时同时服务管理台） |
| `fuyao-admin init-admin`      | 首次运行身份签发；打印一次性初始化码（兑换后绑定通行密钥） |
| `fuyao-admin login-code`      | 为已有管理员签发身份确认/恢复码                            |
| `fuyao-admin revoke-sessions` | 撤销用户会话                                               |
| `fuyao-admin register-theme`  | 登记或刷新主题目录条目                                     |
| `fuyao-admin validate-theme`  | 依契约校验主题根                                           |

## 构建验证

```bash
# 后端（需要 PostgreSQL 测试库；见 backend/README.md）
cd backend
gofmt -l . && go vet ./... && go build ./...
FUYAO_ADMIN_TEST_DSN='postgres://fuyao_test@/fuyao_mvp_test?host=/tmp/pgsock&port=55432&sslmode=disable' \
  go test ./... -count=1

# 真实主题联调（可选；不设时六个真实构建测试跳过）
FUYAO_TEST_THEME_ROOT=/path/to/FuyaoHomepage/site \
  go test ./internal/httpapi -count=1 -run 'TestBuildPreviewClosedLoop|TestPublishRollbackClosedLoop'
```

## 架构

```
管理台 (React) ──/api──▶ Go 管理 API ──▶ PostgreSQL（站点、主题、构建、发布、任务）
                            │
                            ├─ 内容库（逐站目录树：content/、config/、favicon/）
                            ├─ 构建执行器（注册主题根的任务副本、
                            │   冻结快照、密封产物）
                            ├─ 预览 origin（环回、逐构建、令牌门控）
                            ├─ 发布管理器（密封 release、原子 live 指针）
                            ├─ 媒体任务（主题 Node 索引器，只读）
                            └─ 边缘管理器（渲染并应用受管 Caddy 片段）
```

权威契约位于 `contracts/`：`openapi/management.yaml`（HTTP 面，配套生成的 TypeScript 类型与路由覆盖测试）与 `theme/`（manifest、build-input、release 三份 JSON Schema 及公开 fixtures，Go 校验器与主题 Node 工具消费同一份契约）。

## 项目结构

见 [PROJECT_STRUCTURE_ZH.md](./PROJECT_STRUCTURE_ZH.md)。迭代台账见
[docs/ROADMAP_ZH.md](./docs/ROADMAP_ZH.md)。

## 部署

服务器模板（systemd 单元、环境样例、Caddy apply 辅助脚本）位于
[deploy/fuyao-admin/](./deploy/fuyao-admin/)；该处双语 README 说明小内存构建约束与受管片段拓扑。当前已部署服务器仍运行拆分前构建；切换方案见路线图。

## 许可证

[AGPL-3.0-only](./LICENSE)。主题项目保留各自许可证。
