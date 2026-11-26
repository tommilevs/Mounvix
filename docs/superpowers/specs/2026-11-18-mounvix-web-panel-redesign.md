# Mounvix Web Panel — Visual Refresh and Theme

**Status:** Implemented, QA-verified, and committed locally as `6e1c11d`; no push/deploy.
**Date:** 2026-11-18 (Europe/Moscow)
**Scope:** Go-rendered server panel in `server/internal/app`.

## Goal

Replace the cramped, light-only admin-template appearance with a legible, responsive Mounvix operator console. Keep the existing panel behavior and routes intact while adding a persistent **Dark / Light / System** selector, defaulting new browsers to Dark.

## Evidence and diagnosis

The supplied 1919×1042 Overview screenshot shows a long row of twelve links in a shallow navy header, a narrow 1100px centered content area, five generic white metric cards and a small audit table, surrounded by a large light canvas. The current CSS is an inline `styleCSS` constant in `server/internal/app/panel.go`; all private pages share `head.html`/`foot.html`, while login has its own template. There is no theme selector or theme script. The layout and missing theme do not meet the product spec's mobile-friendly, system/light/dark requirements (§6.2).

## Approved direction

- **Visual thesis:** a compact Mounvix operator console, anchored in the product's existing navy/blue, with a dark ink-blue canvas, clear luminance steps, restrained blue actions and no decorative gradients or glass. The goal is precise scanning, not a marketing landing page.
- **Content plan:** persistent left navigation → page heading and existing task content → existing status/record details. The Overview keeps its real version/uptime/root/user/session numbers and audit rows; do not invent health data or fill whitespace with new content.
- **Interaction thesis:** navigation is immediate; the theme picker applies immediately and persists locally. No decorative page-load motion. Buttons may use brief press feedback; honor `prefers-reduced-motion`.
- **Navigation:** a left sidebar on wide screens, with the current page identified accessibly. On smaller screens, a compact top bar opens the same navigation; keep every existing route available. The user explicitly selected the sidebar direction.
- **Theme:** new sessions start in Dark; users may choose Light or System. System follows `prefers-color-scheme`. Save preference per browser in `localStorage` under `mounvix_theme`; no server config, cookie or account coupling.
### Palette and roles

Keep the existing Mounvix navy/blue accent family. Dark is the CSS fallback and first-visit mode; the System choice selects the table's matching palette through `prefers-color-scheme`.

| Token | Light | Dark | Role |
|---|---|---|---|
| `--canvas` | `oklch(0.965 0.008 250)` | `oklch(0.16 0.015 250)` | Page background |
| `--sidebar` | `oklch(0.93 0.015 250)` | `oklch(0.19 0.02 250)` | Persistent navigation |
| `--surface` | `oklch(0.995 0.003 250)` | `oklch(0.23 0.018 250)` | Forms and content surfaces |
| `--surface-raised` | `oklch(0.945 0.012 250)` | `oklch(0.27 0.022 250)` | Selected/raised rows |
| `--text` | `oklch(0.24 0.018 250)` | `oklch(0.93 0.008 250)` | Primary text |
| `--muted` | `oklch(0.55 0.018 250)` | `oklch(0.70 0.015 250)` | Secondary text |
| `--border` | `oklch(0.87 0.015 250)` | `oklch(0.34 0.018 250)` | Dividers and field outlines |
| `--accent` | `oklch(0.44 0.09 250)` | `oklch(0.75 0.08 250)` | Mounvix blue action/link |
| `--accent-hover` | `oklch(0.38 0.09 250)` | `oklch(0.82 0.08 250)` | Hover/active emphasis |
| `--accent-soft` | `oklch(0.90 0.04 250)` | `oklch(0.30 0.04 250)` | Current navigation item |
| `--on-accent` | `oklch(0.985 0.003 250)` | `oklch(0.18 0.015 250)` | Text on primary actions |
| `--focus` | `oklch(0.56 0.13 250)` | `oklch(0.78 0.10 250)` | Keyboard focus outline |
| `--success` | `oklch(0.53 0.12 150)` | `oklch(0.78 0.10 150)` | Existing success states |
| `--warning` | `oklch(0.65 0.13 75)` | `oklch(0.82 0.09 75)` | Existing warning states |
| `--danger` | `oklch(0.55 0.16 25)` | `oklch(0.72 0.13 25)` | Errors/destructive actions |

### Typography

Retain `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`; no downloaded fonts. Body 15px/1.5; page heading 28px/1.2 at weight 650; section heading 20px/1.3 at weight 600; labels and table text 14px/1.4; helper text 13px/1.45. Use `font-variant-numeric: tabular-nums` for counters and timestamps.

### Component styling, depth and layout

Desktop sidebar 240px, sticky full-height, content maximum width 1240px and fluid page padding `clamp(16px, 2.2vw, 32px)`. Spacing steps are 4/8/12/16/24/32px; radii are 4/8/12px plus pill. Buttons, inputs and selects have a 40px minimum hit area, 8px radius, visible focus and consistent heights. Tables use 14px type, left-aligned labels, tabular numeric values, restrained dividers and horizontal scrolling on phones. Do not turn every item into an identical card: Overview metrics are a compact aligned strip separated by hairlines. Elevation comes from background luminance steps; no default drop shadows in dark mode.

### Project guardrails

1. Keep all visible copy and existing product data; add only translated theme and navigation-control labels.
2. Preserve current navy/blue identity; avoid purple/cyan-on-dark, gradients and glass.
3. No external font/icon/network request; CSS and JS are embedded locally.
4. No route, permission, handler, form or API behavior changes.
5. Theme switching is immediate; do not animate a keyboard-triggered theme change.
6. Keep mobile navigation collapsed by default below 880px and preserve touch targets.
7. Use `:focus-visible`; never remove focus outlines without replacement.
8. Support `prefers-reduced-motion`; animate only exact transform/opacity properties where press feedback is used.

### Prompt guide for future UI work

- “Restyle an Overview metric row using `--canvas`, `--text`, `--muted`, `--border`, and `--accent`; keep metrics in a single aligned strip, 22px tabular values, 14px labels, 16px gaps, 8px radius, and do not add cards or data.”
- “Style a management form with `--surface`, `--text`, `--muted`, `--border`, `--accent`, and `--focus`; 15px system UI, 8px control radius, 40px minimum control height, visible labels, 4.5:1 text contrast, and a one-column layout below 560px.”
- “Style a data table with 14px system UI, 16px column gaps, left-aligned labels, right-aligned tabular numbers, `--border` row dividers, and horizontal scrolling below 880px; preserve all table markup and server values.”
- **Forms and tables:** shared input/button/focus treatment across every panel page. Preserve labels, names, methods, form actions and server-side validation. Make wide tables horizontally scrollable on narrow screens without changing their data or semantics.

## Technical constraints

- Continue Go `html/template`; no SPA, CSS framework, Node runtime, external CDN, webfont or third-party icon dependency.
- Bundle small local CSS and vanilla JS assets into the Go server binary with `go:embed`; serve them at stable same-origin routes.
- Share the shell across authenticated pages, registration and login. Keep route handlers, permissions, form semantics, page data, translations and API behavior unchanged.
- The public `/s/<token>` share/password/file pages are separate from the management panel and are explicitly out of this refresh.

## Accessibility and responsive behavior

- Semantic `aside`/`nav`/`main`, skip-to-content link, visible `:focus-visible`, `aria-current`, a labeled theme selector and a mobile menu button with `aria-expanded`. While the mobile drawer is open, keep keyboard focus within it and make the background shell and skip link inert; restore focus to the toggle when closing.
- Keep text contrast at least 4.5:1 and interactive hit targets at least 40×40px (prefer 44px on touch).
- Wide layout at 1280px and above; sidebar collapses below approximately 880px; forms become one-column and tables scroll horizontally at phone widths. Test 320px, 375px, 1280px and 1920px.
- Test RU and EN, Dark/Light/System, and a first load with no saved theme. Do not rely on JavaScript for the underlying page content or form operation.

## Acceptance criteria

1. Every management-shell page (Overview, folders, users, shares, media, backup, requests, plugins, AI, settings, logs, stats, login and registration) renders the shared responsive shell and same-origin CSS/JS.
2. Dark is the new-browser default; Light and System work; changing the select persists across reloads and System tracks OS preference.
3. The 12-link menu is usable on desktop and mobile; keyboard and screen-reader state are exposed; no horizontal page overflow at 320px.
4. Existing routes, permissions, text, forms and POST behavior remain unchanged.
5. Go unit/integration tests, server build, and browser visual/interaction QA pass. No deployment or push to Mounvix origin is authorized.
