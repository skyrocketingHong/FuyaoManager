[English](./UI_REFERENCE.md) | **简体中文**

# 管理台界面规范与复用来源

FuyaoManager 采用机器人平台的 208px 侧栏、顶部操作栏和独立滚动工作区。侧栏依次呈现品牌、当前作用域、固定入口、分组菜单与页脚。顶部承载筛选、页签和主要操作，右侧显示带来源标签的应用版本，点击可查看运行服务版本和提交。窄屏使用导航抽屉和可换行的操作栏。

设计令牌由现有 Ant Design 主题和 CSS 变量共同驱动。常规卡片用描边与底色分层，表单和档案可在宽容器内按 8:4 或 6:6 分列，窄屏回到自然文档流。独立列表使用 `TablePanel` 与 `DataTable`，长值保留完整内容，表头在内容滚动时保持可见。需要横向滚动的表格提供有名称的键盘可聚焦区域。工作区仅在边缘还有隐藏内容时渐隐。状态色独立于强调色，编号和版本使用等宽文本，字段名和危险后果保持可见。

页面路径标识主页面及站点，页签和可分享的筛选用查询参数。新增子页应经 `PageTabs` 和 `usePageQuery` 更新地址。浏览器前进、后退、刷新及登录跳转均可恢复子页。草稿确认、一次性操作和选中待删除的行保存在页面状态中。文章编辑及正文表格沿用原有组件和样式，分类、标签和媒体切换改用 `?tab=`，原地址会重定向。

| 来自 robot-platform 的实现 | FuyaoManager 中的落点 |
| --- | --- |
| `docs/架构规范/前端页面布局规范.md`（`b1db1edd`）与 `libraries/ui/styles/panel.css` | `styles/pages.css` 的响应式 12 列网格、常规面板与独立列表 |
| `frontend/lib/workspaceNavigation.ts`，提交 `4d5a679e` | `lib/navigation/query.ts`，由页面校验并更新查询参数 |
| `modules/frontend-updates/ui/src/PlatformComponentsPage.tsx`，提交 `c838b03f` | 平台更新和 GitHub 主题来源复用维护表格，用应用版本作为主显示 |
| `frontend/components/platform/operationbar/TopBar.tsx`，提交 `1e79d584` | `TopBar`、`PlatformVersion` 和 `VersionBadge`，展开查看组件及构建来源 |
| `libraries/ui-sdk/src/scrollFade.ts` | `lib/ui/scrollFade.ts` 的滚动边缘计算，用于工作区及共享表格 |
| `libraries/platform-ui/components/AppearanceProvider.tsx` | 管理台浏览器级明暗模式、12 个强调色预设和自定义颜色 |

页面入口集中在 `App.tsx` 和 `lib/navigation`，页面业务放在 `pages/` 及对应的 `features/`，表格、面板、版本入口和顶栏插槽留在 `components/`。应用入口通过 `StyleProvider layer` 让共享样式与组件令牌参与明确的级联。站点权限、会话、日期偏好与静态发布沿用 FuyaoManager 的业务边界。
