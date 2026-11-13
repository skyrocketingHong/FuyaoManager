[English](./CONFIGURATION.md) | **简体中文**

# 配置参考

全部配置经环境变量驱动。`backend/config.example.env` 是本地开发模板；`deploy/fuyao-admin/config.example.env` 是服务器模板。真实 DSN 与密钥绝不入库。

## 服务

| 变量                          | 默认值                  | 含义                                                                  |
| ----------------------------- | ----------------------- | --------------------------------------------------------------------- |
| `FUYAO_ADMIN_DATABASE_URL`    | —（必填）               | PostgreSQL DSN。                                                      |
| `FUYAO_ADMIN_ADDR`            | `127.0.0.1:8787`        | 监听地址。非环回主机必须列入 `FUYAO_ADMIN_TRUSTED_HOSTS`。            |
| `FUYAO_ADMIN_TRUSTED_HOSTS`   | 空                      | 允许非环回绑定与明文 HTTP origin 的主机列表（如 Tailscale 地址）。    |
| `FUYAO_ADMIN_RP_ID`           | `localhost`             | WebAuthn 依赖方 ID（兼容路径；IP 字面量部署下通行密钥端点返回 503）。 |
| `FUYAO_ADMIN_RP_DISPLAY_NAME` | `FuyaoManager`          | 依赖方显示名。                                                        |
| `FUYAO_ADMIN_RP_ORIGINS`      | `http://localhost:5174` | 接受的 origin；出现 https origin 时会话 Cookie 切换为 `Secure`。      |
| `FUYAO_ADMIN_SESSION_TTL`     | `12h`                   | 会话有效期。                                                          |
| `FUYAO_ADMIN_CHALLENGE_TTL`   | `5m`                    | WebAuthn challenge 有效期。                                           |
| `FUYAO_ADMIN_PERMIT_TTL`      | `10m`                   | 登记许可有效期。                                                      |
| `FUYAO_ADMIN_AUTH_CODE_TTL`   | `10m`                   | 一次性 CLI 码有效期（身份确认与恢复入口）。 |
| `FUYAO_ADMIN_REGISTRATION_SESSION_TTL` | `30m` | 受限登记会话有效期（账号在当前 RP 下无凭据时由兑换签发）。 |
| `FUYAO_ADMIN_UI_DIR`          | 空                      | 已构建管理台目录；非 `/api` 路径以受限 CSP 服务该 SPA。               |

## 路径与契约

| 变量                          | 默认值                            | 含义                                               |
| ----------------------------- | --------------------------------- | -------------------------------------------------- |
| `FUYAO_ADMIN_REPO_ROOT`       | 按标志向上发现                    | 锚定下列默认值与内置契约目录。显式值由运维自定：任何已存在目录均可（仅二进制安装无需本仓标志）；未设置时从工作目录向上发现，无法发现则启动报错。 |
| `FUYAO_ADMIN_THEME_CONTRACTS` | 存在时取 `<repo>/contracts/theme` | 权威主题契约。显式配置但损坏的目录会导致启动失败。 |
| `FUYAO_ADMIN_SITES_ROOT`      | `<repo>/data/sites`               | 逐站 `content/`、`config/`、`favicon/` 目录树。    |
| `FUYAO_ADMIN_BUILDS_ROOT`     | `<repo>/data/builds`              | 逐任务目录（快照 + 产物 + 日志）。                 |
| `FUYAO_ADMIN_RELEASES_ROOT`   | `<repo>/data/releases`            | 密封的逐站 release 目录。三个根必须解析为三个相互独立的真实目录；供应写入绝不覆盖既有站点文件。 |

## 构建、预览、边缘

| 变量                                            | 默认值                                      | 含义                                                                                          |
| ----------------------------------------------- | ------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_BUILD_MODE`                        | `ci`                                        | `ci` 使用主题 fixture 内容；`production` 需要逐站内容根（由 `FUYAO_ADMIN_SITES_ROOT` 推导）。 |
| `FUYAO_ADMIN_REQUIRE_CAPABILITIES`              | `blog`                                      | 受信的服务端能力要求，主题必须覆盖。                                                          |
| `FUYAO_ADMIN_REQUIRE_URL_CONVENTIONS`           | `posts,blog-rss,sitemap`                    | 受信的 URL 约定要求。                                                                         |
| `FUYAO_ADMIN_PREVIEW_BIND`                      | `127.0.0.1`                                 | 预览监听绑定地址。                                                                            |
| `FUYAO_ADMIN_PREVIEW_EXTERNAL_BASE`             | 空                                          | 预览交给浏览器的 URL base（有远程客户端时绝不能是 127.0.0.1 字面量）。                        |
| `FUYAO_ADMIN_PORT_MIN` / `FUYAO_ADMIN_PORT_MAX` | `20000` / `20100`                           | 站点入口与预览的平台分配端口范围。                                                            |
| `FUYAO_ADMIN_DEPLOYMENT_ENABLED`                | `false`                                     | 默认运行隔离：建站、内容、构建与预览可用，但不启动发布服务器、不应用 Caddy 片段，发布/回滚/可见性/边缘重应用返回 `503 deployment_disabled`。历史发布仍可读。 |
| `FUYAO_ADMIN_EDGE_SNIPPET_DIR`                  | 空                                          | 启用受管 Caddy 片段（由数据库状态渲染）；发布开关关闭时不生效。                                |
| `FUYAO_ADMIN_CADDY_CONTROL_CMD` | 空 | 运维固定的逗号分隔 argv，调用 root 拥有的源配置能力，经 stdin 接收受限 JSON；HTTP 不可选择命令或路径。 |
| `FUYAO_ADMIN_CADDY_MANAGEMENT_ENABLED` | `false` | 仅启用已登记静态网站的源配置校验后部署和回退，与主题发布流水线分开控制。 |
| `FUYAO_ADMIN_EDGE_APPLY_CMD`                    | `sudo -n /usr/local/sbin/fuyao-caddy-apply` | 运维控制的校验 + reload 命令（固定参数）。                                                    |
| `FUYAO_ADMIN_EDGE_PRIVATE_BIND`                 | `127.0.0.1`                                 | 私有站点入口的精确绑定地址。                                                                  |
| `FUYAO_ADMIN_EDGE_PUBLIC_BIND`                  | 空                                          | 公开入口绑定（空 = 全接口）。                                                                 |
| `FUYAO_ADMIN_EDGE_PUBLIC_HOST`                  | 空                                          | 公开入口对外通告的主机。                                                                      |

## 仅测试

| 变量                    | 含义                                                                                                                                                           |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `FUYAO_ADMIN_TEST_DSN`  | 集成测试 DSN；测试拒绝 `fuyao_mvp_test` 以外的任何数据库。未设置时使用 `<repo>/.fuyao/cms-mvp/postgres-test.json` 的任务本地描述符，否则 PostgreSQL 测试跳过。 |
| `FUYAO_TEST_THEME_ROOT` | 外部真实主题根（已安装依赖的 FuyaoHomepage `site/` 检出），启用六个真实 Node 构建集成测试；未设置时带明确指引跳过。                                            |

## Cookie 说明

明文 HTTP 拓扑（环回开发与受信私网地址）下 Cookie 绝不带 `Secure`——浏览器会静默丢弃明文 HTTP 上的 Secure Cookie。仅当全部配置 origin 均为 https 时才启用 `Secure`。
