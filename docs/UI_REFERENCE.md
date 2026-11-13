English | [简体中文](./UI_REFERENCE_ZH.md)

# Console UI reuse

The console reuses generic interface code from the user-selected `robot-platform/frontend`. The read-only reference revision for this iteration is `8644ba55c7c1963892518ee77cc6bd18146144e9`. The reference repository was not modified.

| Reference module (relative to its frontend)                                          | Manager implementation                                     | Reused behavior                                                                                              |
| ------------------------------------------------------------------------------------ | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `components/platform/sidebar/SideMenu.tsx`, `SidebarActions.tsx`                     | `src/components/layout/AppShell.tsx`, `SidebarActions.tsx` | 208px sidebar, brand → information → fixed actions → scrolling menu → footer, separate return action         |
| `components/platform/sidebar/InfoCard.tsx`, `SidebarFoot.tsx`                        | `SidebarInfo.tsx`, `SidebarFoot.tsx`                       | Title and two-column facts, version region and fading separators                                             |
| `components/platform/statusbar/TopBar.tsx`, `styles/statusbar/statusbar.css`         | `TopBar.tsx`, `src/styles/statusbar/statusbar.css`         | 44px single-row bar, 32px controls, page/action slots, layered background and account button                 |
| `components/platform/shared/Widgets.tsx`                                             | `src/components/DataTable.tsx`                             | Small table density and shared surface; manager row types, stable keys, sorting and pagination remain intact |
| `styles/shared/panel.css`, `forms.css`, `controls.css`, `styles/sidebar/sidebar.css` | `src/styles/pages.css`, `src/styles/sidebar/sidebar.css`   | Panel anatomy, table density, form grids, controls and sidebar regions                                       |
| Application entry's `StyleProvider layer`                                            | `src/main.tsx`                                             | Layered antd CSS gives the shared rules their intended precedence                                            |

Functional icons use the reference's Lucide 1.31.0; brand icons use Simple Icons. React 18, manager routing, authentication and locale state remain in use. Device controllers, telemetry, company branding and the fixed 16:9 stage are outside the copied modules; the workspace remains responsive.

Sidebar facts come from the caller's actual accessible sites and membership roles. Loading and failure states do not invent counts. The cache is scoped by authentication generation and rejects previous-account responses. Locale changes rerender page components without discarding drafts. Site-configuration saves lock editing and install canonical server values after success.

This iteration also fixes React #310 caused by calling `usePageHeader` only after the site-configuration load and supplies a recoverable route-error page. Verification covers source, types, real router/header state-transition tests and production builds. Visual and physical-authenticator acceptance remain with the user.
