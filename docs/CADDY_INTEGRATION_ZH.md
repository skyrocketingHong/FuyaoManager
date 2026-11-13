[English](./CADDY_INTEGRATION.md) | **简体中文**

# Caddy 站点与预览入口

平台通过运维配置的固定只读命令调用 Caddy 自己的 adapter，解析真实配置 JSON 后登记站点。同步只更新管理数据库，不修改网站目录、主题源或原 Caddy 规则。首次登记的站点归同步管理员所有，其他账号通过既有站点授权获得访问权限。管理域名及其控制子域不作为业务站点导入。

站点按静态、重定向、反向代理及其他配置分类，保留实际域名、监听、可识别的根目录和跳转目标。页面不会把组合配置重新渲染成简化模板。已从源配置移除的站点标为缺失，不自动删除其授权和历史。登记记录中的私有可见性和ready状态是平台元数据，实际监听与来源状态以Caddy快照为准。

平台首页区分平台管理和 Caddy 配置管理。仅有实际目录的静态网站可以转换，转换前由运维登记完整源配置和不可由应用修改的模板。重定向、反向代理及未知类型保留在 Caddy 配置管理。转换只保存当前设置，不修改在线配置、网站内容或发布指针，也不自动选择主题。

已登记源的配置页分别执行保存草稿、校验当前修订、部署及恢复已应用修订。强 ETag 防止旧页面覆盖，源文件摘要检测外部改动。压缩、缓存策略及基础响应头均为有限设置，按运维登记逐项开放；保留模式维护完整原规则及共享 snippet。HTTP 不接受任意 Caddyfile 文本、路径、上游地址或命令。应用失败恢复最后良好源并报告失败。原始配置、校验计划和 root 修订保存在应用账号不可访问的受保护目录，浏览器仅收到安全设置和历史元数据。

主题工作流与外部 Caddy 站点分开。主题站点保留内容、主题参数、构建和发布工作区；外部静态站点显示标准配置，重定向站点显示跳转目标。服务端同时拒绝对非主题站点调用内容和构建接口。Caddy 的 file_server 不能用于推断某个已登记主题。

管理服务使用普通账号运行。只授权 root 所有、无调用参数的固定 helper；源配置控制使用独立安装、root 拥有的二进制，通过 stdin 接收受限 JSON，并以 caddy 用户执行校验和重载，不提供通用 shell。Caddy 管理接口使用受文件权限保护的 Unix socket。写入需要完整管理员会话、站点所有者及 CSRF，并在事务内重检。原配置文件和证书不通过 HTTP 暴露。`FUYAO_ADMIN_CADDY_MANAGEMENT_ENABLED` 独立启用已登记站点的源配置部署，主题发布开关可以继续关闭。

清单脱敏程序与源配置控制程序一并安装，快照 helper 只输出版本化安全清单，不把完整 Caddy JSON 交给应用账号。源配置托管部署撤销旧的任意片段写权限和通用应用 sudo 授权；主文件、源模板和控制二进制由 root 持有。应用修订时，重载前后均核对 Unix socket 中的实际运行配置；保留模式也会产生新 root 修订，仅比较源摘要不足以识别跨层提交失败。数据库与 root 修订不一致时停止后续变更，由运维核对恢复。

预览地址为 `https://preview.manager.fuyaoskyrocket.ing/正式域名/原路径?lease=短期授权`；没有正式域名的主题站点使用其slug。预览是独立HTTPS源，管理会话Cookie不发送给它。受控外层页面将原始产物放入独立HTTPS端口的iframe，保留SvelteKit等框架的生产路径；不会替换产物JavaScript或改变正式发布base。后台Go端口与外部TLS端口通过运维偏移映射，避免占用站点发布监听。

关闭会取消在途签发、撤销授权、停止后端监听并释放分配。已有租约重载只重新授权同一有效实例，关闭后的wrapper返回410，不会自动创建新实例。Caddy在后端关闭时返回不可缓存的410；预览vhost跳过访问日志，默认日志过滤URI及请求头，避免租约和入口令牌落盘。

HTTPS管理会话和认证事务使用`__Host-` Cookie，Secure、HttpOnly、Path=/且无Domain，拒绝旧名和同名多Cookie。升级后已有用户需重新登录；通行密钥和账号不变。HTTP localhost 保持开发兼容。

平台API页面读取认证后的`GET /api/v1/openapi`，权威源为`contracts/openapi/openapi.yaml`。目录只显示契约声明，不推断未声明版本或权限。

## 源配置控制程序安装

在 `backend/` 为服务器架构构建 `./cmd/fuyao-caddy-control` 和 `./cmd/fuyao-caddy-inventory`。两个二进制由 root 安装至 `/usr/local/libexec/`，`deploy/fuyao-admin/` 的同名 wrapper 安装至 `/usr/local/sbin/`；应用账号不能替换文件或任何父目录。sudo 只授权零参数 `fuyao-caddy-control` 和 `fuyao-caddy-snapshot`，移除旧通用 apply 授权及应用对 Caddy 导入片段的写 ACL。安装 sudoers 前执行 `visudo -cf`。

每个可接管站点对应 root 拥有的 `/etc/fuyao-caddy-control/sites/<sourceKey>.json`。使用清单中的确切 source key，不从域名猜测。登记包含 `siteKey`、`kind: static`、`staticRoot`、`masterFile`、`sourceFile`、`baseTemplate`、`masterTemplate`、`initialDigest`、`capabilities` 和 `preserve`。路径均由运维控制；静态目录必须实际存在，源文件、模板、主文件及父目录须由受信账号持有且不可被应用修改。

原样抽出完整 host 块，在主文件原位置改为导入受保护源文件的绝对路径，保留共享 snippet 和其他站点。不可变基础模板只替换已开放的 `__FUYAO_COMPRESSION__`、`__FUYAO_CACHE_CONTROL__`、`__FUYAO_SECURITY_HEADERS__` 槽位；`preserve` 保存各槽位原始片段，全部选择保留时须与原源文件逐字节一致。主模板仅替换唯一 `__FUYAO_SITE_SOURCE__`。切换前比较完整 adapter 配置，仅批准受保护配置文件新增的自动 hide 路径，其余任何差异均拒绝。

Caddy 账号需要读取和遍历在线源及校验工作文件，无需写权限；登记、模板和 `/var/lib/fuyao-caddy-control/` 不对应用账号开放。控制程序固定使用 `/run/caddy/admin.sock`，适配命令使用运行中 Caddy 的环境，避免丢失解析期秘密。完成上述校验后才启用独立源配置开关。主模板或其他导入配置发生变化会使已有校验计划失效，可能需要运维核对。

sudo helper 继承应用服务的文件系统 namespace。使用 `ProtectSystem=full` 时，只将 root 拥有的源目录纳入精确 `ReadWritePaths`，Unix 权限仍须拒绝应用账号直接写入；模板和二进制继续只读。root 终端成功不能代替服务上下文验证，须通过真实认证管理 API 执行部署闭环。
