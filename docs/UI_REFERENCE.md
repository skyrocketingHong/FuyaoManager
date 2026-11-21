**English** | [简体中文](./UI_REFERENCE_ZH.md)

# Console design and implementation references

FuyaoManager uses the robot platform's 208px sidebar, operation bar and independently scrolling workspace. The sidebar presents the brand, current scope, fixed entry points, grouped navigation and footer. Filters, tabs and primary actions sit in the operation bar. The version control on the right identifies the component and opens the running service version and commit. Narrow screens use a navigation drawer and a wrapping operation bar.

Ant Design tokens and CSS variables share the console's theme. Panel borders and surface colors set the hierarchy. Forms and records can use 8:4 or 6:6 columns in wide containers and follow natural document flow on narrow ones. Standalone lists use `TablePanel` and `DataTable`, with sticky headers and room for long values. Tables that scroll horizontally expose a named, keyboard-focusable region. Overflow fades indicate hidden content at each vertical edge. Status colors are independent of the accent. Versions and identifiers use monospace text. Field labels and the consequences of destructive actions stay visible.

Paths identify primary pages and site scopes. Query parameters identify tabs and shareable filters. New subviews update the address through `PageTabs` and `usePageQuery`. Back, forward, reload and login restore the selected view. The UI keeps draft confirmations, one-time actions and bulk selections in local state. Article editing and tables retain their existing components and styles. Categories, tags and media use `?tab=`, with redirects for previous links.

| robot-platform implementation | FuyaoManager implementation |
| --- | --- |
| `docs/架构规范/前端页面布局规范.md` (`b1db1edd`) and `libraries/ui/styles/panel.css` | Responsive 12-column grid, ordinary panels and standalone lists in `styles/pages.css` |
| `frontend/lib/workspaceNavigation.ts`, commit `4d5a679e` | Page-owned and validated query parameters in `lib/navigation/query.ts` |
| `modules/frontend-updates/ui/src/PlatformComponentsPage.tsx`, commit `c838b03f` | Shared maintenance tables for application and GitHub theme updates, organized by product version |
| `frontend/components/platform/operationbar/TopBar.tsx`, commit `1e79d584` | `TopBar`, `PlatformVersion` and `VersionBadge` with expandable build details |
| `libraries/ui-sdk/src/scrollFade.ts` | Scroll edge calculations for the console workspace and shared tables in `lib/ui/scrollFade.ts` |
| `libraries/platform-ui/components/AppearanceProvider.tsx` | Browser-local light and dark modes, twelve accent presets and a custom color |

`App.tsx` and `lib/navigation` own page entry points. Feature code lives in `pages/` and the corresponding `features/`. Tables, panels, version controls and operation-bar slots belong in `components/`. The entry point uses `StyleProvider layer` to give shared styles and component tokens explicit cascade precedence. Site permissions, sessions, date preferences and static publishing follow FuyaoManager's own contracts.
