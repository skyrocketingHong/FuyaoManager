[English](./UI_REFERENCE.md) | **简体中文**

# 管理台界面规范与复用来源

FuyaoManager 采用机器人平台的桌面外壳：侧栏有 208px 展开、52px 图标栏和隐藏三档，顶部为操作栏，工作区独立滚动。侧栏依次呈现品牌、当前作用域、固定入口、分组菜单与页脚。折叠为图标栏时常驻图标和至多两行的分组标题，各入口保留可访问名称；鼠标悬停或键盘聚焦时完整菜单浮出并覆盖工作区，页面占位不变，按 Esc 收起，鼠标移出或焦点再次进入后恢复。隐藏档在操作栏左端提供恢复按钮，图标栏状态下该处显示品牌标。模式按浏览器保存。操作栏承载筛选、页签和主要操作，右侧显示应用版本，点击可查看运行服务版本和提交；气泡无箭头，表面在 420 毫秒内由胶囊尺寸形变至详情尺寸，收起时形变回胶囊。窄屏使用导航抽屉和可换行的操作栏。切换页面时旧页 100 毫秒淡出、新页 200 毫秒淡入；只改页签或筛选、替换式导航、减少动态或不支持 View Transition 时直接切换。

设计令牌由现有 Ant Design 主题和 CSS 变量共同驱动，运动时长分 100、200、300 毫秒三档（`--motion-fast`、`--motion-mid`、`--motion-slow`），顶栏版本气泡的 420 毫秒形变是机器人平台为胶囊浮层规定的例外。输入与选择控件在 992px 及以上高 34px，更窄时保持 44px。常规卡片用描边与底色分层，表单和档案可在宽容器内按 8:4 或 6:6 分列，窄屏回到自然文档流；表单栅格最多两列，列距 20px。独立列表使用 `TablePanel` 与 `DataTable`，长值保留完整内容，表头在内容滚动时保持可见。需要横向滚动的表格提供有名称的键盘可聚焦区域。工作区和表格内容区仅在边缘还有隐藏内容时渐隐，强制高对比模式下取消渐隐。状态标签有成功、警告、危险、信息和中性五种语义，颜色独立于强调色，底色与描边混入所在表面；标签可换行，表格单元格内保持单行。加载状态默认播报“加载中”。编号和版本使用等宽文本，字段名和危险后果保持可见。显示设置可为内容区选择个人底色：主题底色占 90%，个人颜色混入 10%。

页面路径标识主页面及站点，页签和可分享的筛选用查询参数。新增子页应经 `PageTabs` 和 `usePageQuery` 更新地址。浏览器前进、后退、刷新及登录跳转均可恢复子页。草稿确认、一次性操作和选中待删除的行保存在页面状态中。页面内的路由切换由 react-router 的导航拦截器保护；退出登录先经 `lib/editing/leaveGuard` 询问活动中的草稿守卫，确认后才清除会话。文章编辑及正文表格沿用原有组件和样式，分类、标签和媒体切换改用 `?tab=`，原地址会重定向。

每个路由槽位由 `PageBoundary` 包裹。页面渲染异常时在工作区内报错并提供重试，侧栏、操作栏和会话保持挂载；位置变化后清除错误状态。

| 来自 robot-platform 的实现 | FuyaoManager 中的落点 |
| --- | --- |
| `docs/架构规范/前端页面布局规范.md`（`b1db1edd`）与 `libraries/ui/styles/panel.css` | `styles/pages.css` 的响应式 12 列网格、常规面板与独立列表 |
| `frontend/components/platform/sidebar/SideMenu.tsx`、`sidebarMode.ts` 与 `frontend/styles/sidebar/sidebar.css`（`aa21ec10`） | `DesktopSidebar`、`sidebarMode.ts` 与 `styles/sidebar/sidebar.css` 的侧栏三档和浮出菜单 |
| `frontend/lib/workspaceNavigation.ts`（`4d5a679e`） | `lib/navigation/query.ts`，由页面校验并更新查询参数 |
| `modules/frontend-updates/ui/src/PlatformComponentsPage.tsx`（`c838b03f`） | 平台更新和 GitHub 主题来源复用维护表格，用应用版本作为主显示 |
| `frontend/components/platform/operationbar/TopBar.tsx`（`1e79d584`） | `TopBar`、`PlatformVersion` 和 `VersionBadge`，展开查看组件及构建来源 |
| `libraries/ui-sdk/src/scrollFade.ts` 与 `scroll-fade.css` | `lib/ui/scrollFade.ts` 与 `styles/shell.css` 的滚动边缘计算和渐隐 |
| `libraries/ui-sdk/src/status.css` 与 `loading.css` | `styles/base.css` 的状态标签和加载状态规格 |
| `libraries/ui/styles/base.css` 与 `controls.css` | `styles/base.css` 与 `styles/pages.css` 的界面标题排版、按钮按压反馈和操作按钮禁用提示 |
| `libraries/ui/styles/forms.css` 与 `stage.css` | `styles/pages.css` 的桌面控件密度与两列表单栅格；`lib/navigation/pageTransition.ts` 与 `styles/shell.css` 的页面切换过渡 |
| `libraries/ui/operationbar/StatusPopover.tsx` 与 `pillMotion.ts` | 顶栏 `VersionPopover`：版本详情表面自胶囊形变展开与收起 |
| `libraries/platform-ui/components/AppearanceProvider.tsx` 与 `libraries/platform-ui/lib/appearance.ts` | `lib/theme` 的浏览器级明暗模式、12 个强调色预设、自定义颜色和个人内容底色（`3ce49448`） |

页面入口集中在 `App.tsx` 和 `lib/navigation`，页面业务放在 `pages/` 及对应的 `features/`，表格、面板、版本入口和顶栏插槽留在 `components/`。相对导入只指向同层或下层：`lib`、`components`、`features`、`pages` 依次向上依赖，入口文件不受限制，组合多个页面的组件归 `pages/`。`tests/boundaries.test.ts` 校验导入方向，并校验 `App.tsx` 的页面登记表覆盖全部导航项。页面切换过渡由 `withPageTransitions` 在路由器入口（`main.tsx`）集中启用，各导航调用点与链接无须逐个传 `viewTransition`；只对改变路径的前进导航及对应的前进后退生效。应用入口通过 `StyleProvider layer` 让共享样式与组件令牌参与明确的级联。站点权限、会话、日期偏好与静态发布沿用 FuyaoManager 的业务边界。

## 适配边界

同步基线为 robot-platform `dev` 分支的提交 `7ddbaf0e`（2026-11-24），并已核对至同日的 `0a5e6b8d`：其间的通用界面增量只有登录页极光动效（`b336dd75`），其余为前后端独立发布与边缘主机维护。下列内容属于设备业务、产品主视觉或平台基础设施，FuyaoManager 不移植：工作台 5×3 栅格与卡片、驾驶舱、设备与驱动页面及沙箱运行库、`ContentSurface` 的背景插槽、Kubai 图标引擎、设备状态与快捷胶囊及其特效、展示型登录页的极光、模块化构包与独立发行件。FuyaoManager 自有的内容页动效（相册、友链排序）和窄屏导航抽屉沿用各自既有实现。

有意保留的一处差异：操作栏在窄容器内换行，不横向滚动。这是本项目 2026-11-14 的决定（`6d7b8fa`、`8e001d3`，移除整条筛选区的横向滚动）；机器人平台在 2026-11-19 为单行操作栏补了滚轮横滚与边缘渐隐。
