[English](./PROJECT_STRUCTURE.md) | **简体中文**

# 项目结构与总览

## 总览

FuyaoManager 是 2026-11-12 从 FuyaoHomepage 拆分出的站点与内容管理平台。它管理站点、内容、媒体任务、主题登记、冻结快照构建、私有预览、发布、回滚以及受管反向代理边缘。主题是通过 `contracts/theme/` 版本化契约接入的外部项目；本仓库在没有任何主题检出时即可完成构建与测试（真实主题联调测试经 `FUYAO_TEST_THEME_ROOT` 激活）。

## 目录结构

```text
.
├── .github/workflows/ci.yml   # CI：后端（真实 PostgreSQL）、前端、
│                              # 可选的外部主题联调作业
├── .gitignore                 # 忽略 data/、依赖、构建产物、密钥
├── .npmrc                     # npm engine-strict
├── .prettierignore            # 格式化忽略（锁文件、生成类型）
├── .prettierrc                # Tab + 单引号格式化
├── AGENTS.md                  # 本仓库开发约定
├── CHANGELOG.md               # 英文变更日志
├── CHANGELOG_ZH.md            # 简体中文变更日志
├── CONFIGURATION.md           # 英文环境变量参考
├── CONFIGURATION_ZH.md        # 简体中文环境变量参考
├── PROJECT_STRUCTURE.md       # 英文项目结构
├── PROJECT_STRUCTURE_ZH.md    # 本文档
├── README.md                  # 默认英文入口
├── README_ZH.md               # 简体中文入口
├── LICENSE                    # AGPL-3.0-only
├── package.json               # 根便捷脚本（前端持有独立锁文件）
├── backend/                   # Go 管理后端
│   ├── cmd/fuyao-admin/       # CLI：migrate / serve / init-admin / login-code /
│   │                          # revoke-sessions / bootstrap-admin / create-site /
│   │                          # validate-theme / register-theme
│   ├── internal/
│   │   ├── cli/               # 命令接线 + serve 组装
│   │   ├── config/            # 环境变量加载与校验
│   │   ├── db/                # 连接池 + SQL 迁移（0001-0012）
│   │   ├── model/             # 共享记录
│   │   ├── store/             # 用户、会话、通行密钥、一次性码、站点、
│   │   │                      # 主题、构建、发布、媒体任务、网络入口
│   │   ├── httpapi/           # 管理 API：认证、站点、内容、相册、
│   │   │                      # 主题、构建、发布、边缘状态、UI 托管
│   │   │                      # + testdata/theme（内置测试主题）
│   │   ├── content/           # 逐站受限内容库（ETag、原子写）
│   │   ├── siteprovision/     # 逐站目录供应
│   │   ├── themeregistry/     # 运维驱动的主题登记
│   │   ├── themecontract/     # contracts/theme 加载与校验（Go 侧）
│   │   ├── buildrunner/       # 构建任务、受信适配器、任务工作区、
│   │   │                      # 预览 origin
│   │   ├── publish/           # release 密封、原子 live 指针、已发布 origin
│   │   ├── mediatask/         # 经主题 Node 脚本的相册索引
│   │   ├── edge/              # 受管 Caddy 片段渲染与应用
│   │   ├── wa/                # WebAuthn 桥（兼容路径）
│   │   └── testauth/          # 仅测试用的合成认证器
│   ├── config.example.env     # 无真实凭据的环境模板
│   └── go.mod / go.sum        # module github.com/skyrocketingHong/FuyaoManager/backend
├── frontend/                  # React + antd 6 管理台（独立 package-lock.json）
│   ├── src/lib/api/           # 类型化客户端 + 生成的 schema.d.ts
│   ├── src/lib/sites/         # 站点业务模块（防竞态详情、编辑器）
│   ├── src/lib/themes/        # 主题选择存储（目录、If-Match、412）
│   ├── src/lib/editing/       # EditableDoc 状态机 + 未保存离开保护
│   ├── src/lib/navigation/    # 唯一导航元数据（路由、菜单、作用域）
│   ├── src/lib/webauthn/      # 通行密钥仪式、JSON 转换、错误映射
│   ├── src/lib/i18n/          # 中英文字典
│   ├── src/lib/theme/         # 日夜状态 + 语义令牌装配
│   ├── src/lib/session/       # 会话状态：匿名/受限/完整/错误
│   ├── src/components/        # AppearanceProvider、PageHeader(+PageTabs)、Panel/
│   │                          # TablePanel、PageLoading、StatusTag、layout/ 外壳
│   ├── src/styles/            # base / shell / pages 样式（语义变量）
│   ├── src/pages/             # 登录、通行密钥绑定、主题目录、站点与各业务页
│   └── tests/                 # vitest 测试（jsdom）
├── contracts/
│   ├── openapi/management.yaml  # 权威管理 HTTP 契约
│   └── theme/                   # 权威主题契约
│       ├── manifest.schema.json / build-input.schema.json / release.schema.json
│       ├── README.md / README_ZH.md
│       └── examples/            # 公开安全的正负例 fixtures
├── deploy/
│   └── fuyao-admin/          # 服务器部署：双语 README、systemd 单元、配置样例、
│                             # Caddy apply 脚本、公网静态页（static-page/）、
│                             # 公网 Caddy vhost 模板（caddy/）
└── docs/
    ├── ROADMAP.md / ROADMAP_ZH.md          # 迭代台账（双语）
    ├── SOURCE_MIGRATION.md                 # 拆分记录 + 提交映射指针
    └── source-mapping/commit-map-from-fuyaohomepage.txt
```

## 运行数据布局

默认运行数据位于 `<repo>/data/`（各根可经 `FUYAO_ADMIN_*` 覆盖）：`data/builds/<taskId>/`（快照 + 产物 + 日志）、`data/sites/<siteId>/{content,config}`、`data/releases/<siteId>/<buildId>/`。部署环境将其指向持久数据目录（`/srv/fuyao-admin/data/...`）；`data/` 下内容一律不入版本控制。
