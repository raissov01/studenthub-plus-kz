# Laptop layout: references (2026-11)

Public docs and help pages only; nothing copied (no assets, no copy). What we took is the layout pattern.

| Product | Source | Pattern we reuse |
|---|---|---|
| Material 3 window size classes | [window_size_classes (pub.dev)](https://pub.dev/packages/window_size_classes), [SAP Fiori M3 navigation drawer](https://www.sap.com/design-system/fiori-design-android/v25-8/components/m3-standard-components/navigation-drawer/usage) | Bottom bar on compact, permanent side navigation on expanded widths; the drawer gives way to an expanded rail. Our cut: phone shell < 1024px, sidebar from 1024px. |
| Canvas LMS (student) | [Global Navigation Menu](https://community.canvaslms.com/docs/DOC-10603-how-do-i-use-the-global-navigation-menu-as-a-student), [Dashboard as a student](https://community.canvaslms.com/docs/DOC-4431), [UMT course navigation tip sheet](https://www.umt.edu/canvas/tip-sheets/course-navigation-and-settings-tip-sheet.pdf) | Global menu on the left of every page, collapsible; dashboard in the centre; right sidebar with to-do and upcoming items. → our sidebar (collapsible to icons) and the detail page's right rail (deadline, actions, related). |
| Todoist (web) | [Keyboard shortcuts](https://todoist.com/help/articles/205063212), [Customize the sidebar](https://www.todoist.com/help/articles/6819541084572) | A sidebar toggle in the top-left, single-key shortcuts. → our `/` focuses search, the collapse button sits in the sidebar head. |

Decisions (frontend/src/app/desktop.css, components/SideNav.tsx, lib/desktop.ts):
- ≥1024px: fixed left sidebar (256px, 76px collapsed, remembered on the device), page up to 1200px for list/dashboard screens and 920px for forms and reading screens; the bottom tab bar and the hamburger go away.
- Lists → 2 columns (3 from 1360px); search filters in a sticky side panel instead of a sheet; detail = reading column + sticky 340px rail.
- Home = dashboard grid: search + café, news across, today's classes beside deadlines and a 7-day week strip, quick actions.
- Sheets → centred 560px dialogs (same component, CSS only).
- Flat: white page, 1px borders, pastel fills for active/icon tiles, no shadows/gradients/blur.
