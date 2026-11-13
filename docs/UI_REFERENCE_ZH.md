[English](./UI_REFERENCE.md) | **简体中文**

# 管理台界面复用记录

用户管理及日志页面继续适配 `components/platform/features/settings/UsersPage.tsx` 和 `LogsPage.tsx`：顶部筛选、TablePanel/DataTable、资料编辑、操作确认、会话管理、请求代数守卫及操作文案查找。数据改接本仓类型化接口，认证沿用通行密钥和登记码；平台角色、逐站授权遵循本仓后端契约。

管理台自身的外观设置继续复用 `components/platform/features/settings/AppearancePage.tsx`、`styles/features/settings/appearance.css` 和 `lib/appearance.ts`：日间、夜间、跟随系统、12 种预设强调色、自定义取色及恢复默认。偏好按浏览器保存，兼容旧日夜设置并响应系统模式变化；管理台默认跟随系统，状态颜色独立于强调色。侧栏固定操作和菜单使用相同的 8px 内缩进与 16px 图标列，统一文字对齐。

管理台按用户指定的 `robot-platform/frontend` 复用通用界面代码。本轮只读参考版本为 `8644ba55c7c1963892518ee77cc6bd18146144e9`；参考仓库未修改。

| 参考模块（相对参考项目的 frontend）                                                  | 本仓实现                                                   | 复用内容                                                                   |
| ------------------------------------------------------------------------------------ | ---------------------------------------------------------- | -------------------------------------------------------------------------- |
| `components/platform/sidebar/SideMenu.tsx`、`SidebarActions.tsx`                     | `src/components/layout/AppShell.tsx`、`SidebarActions.tsx` | 208px 侧栏，品牌、信息、固定操作、滚动菜单、页脚的顺序；返回入口与菜单分离 |
| `components/platform/sidebar/InfoCard.tsx`、`SidebarFoot.tsx`                        | `SidebarInfo.tsx`、`SidebarFoot.tsx`                       | 信息标题与双列统计、版本区、渐隐分隔线                                     |
| `components/platform/statusbar/TopBar.tsx`、`styles/statusbar/statusbar.css`         | `TopBar.tsx`、`src/styles/statusbar/statusbar.css`         | 44px 单行顶栏、32px 控件、页面内容和操作插槽、分层背景、账号按钮           |
| `components/platform/shared/Widgets.tsx`                                             | `src/components/DataTable.tsx`                             | `size="small"` 与 `data-table` 表面；保留本仓类型化行、稳定键、排序和分页  |
| `styles/shared/panel.css`、`forms.css`、`controls.css`、`styles/sidebar/sidebar.css` | `src/styles/pages.css`、`src/styles/sidebar/sidebar.css`   | 面板结构、表格密度、表单网格、输入与按钮样式、侧栏分区                     |
| 应用入口的 `StyleProvider layer`                                                     | `src/main.tsx`                                             | 将 antd CSS 放入样式层，使上述共享 CSS 获得预期优先级                      |

功能图标使用与参考项目相同的 Lucide 1.31.0，品牌图标仍使用 Simple Icons。管理台保留 React 18 和既有路由、认证及语言状态；未复制设备控制器、遥测、公司品牌或固定 16:9 舞台。窄屏继续采用响应式工作区。

信息区读取调用者可访问的真实站点列表和站点角色；加载及失败状态不显示虚构数字。列表缓存按会话代数隔离，旧账号响应不能带入新账号。页面构造函数随语言变化重新渲染，草稿不因语言切换卸载。站点资料保存时锁定编辑，成功后采用服务器返回的规范值。

本轮同时修复站点资料页在加载后才调用 `usePageHeader` 导致的 React #310，并加入可重试的路由错误页。验收覆盖源码、类型、真实路由和顶部插槽下的状态转换测试及生产构建；实际视觉与真实认证器操作由用户验收。
