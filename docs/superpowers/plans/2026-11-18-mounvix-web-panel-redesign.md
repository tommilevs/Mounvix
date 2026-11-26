# Mounvix Web Panel Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpower-executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh every Mounvix management-panel page with an accessible responsive sidebar and persistent Dark/Light/System themes, preserving all server behavior.

**Architecture:** Keep server-rendered Go `html/template` pages and existing handlers. Extract the inline stylesheet to a small embedded CSS asset, add a same-origin embedded vanilla-JS asset for theme preference and mobile navigation, and make all panel routes share the responsive shell. No SPA, frontend framework or external resources.

**Tech Stack:** Go 1.22+, `html/template`, `embed`, CSS, vanilla JavaScript, existing `httptest` coverage and Playwright Extension for visual QA.

**Spec:** `docs/superpowers/specs/2026-11-18-mounvix-web-panel-redesign.md`; existing product requirements in `docs/specs/2026-11-17-mounvix-design.md` §6.2 and §17.

## Global Constraints

- Server-rendered pages; no React/SPA or Node runtime on the router.
- No external font, CDN, CSS/JS framework or new runtime dependency.
- New-browser theme default is Dark; Light and System are selectable and saved locally as `mounvix_theme`.
- Preserve routes, permission checks, form methods/actions/field names, server validation, page data and RU/EN localization.
- The public `/s/<token>` share/password/file pages are outside this management-panel refresh.
- No Mounvix-origin push, deployment or live-router mutation.

## Review Focus

- Anonymous login and registration still render the local assets and theme selector without exposing authenticated navigation.
- All authenticated routes retain existing form actions and are visually consistent.
- Mobile navigation exposes accurate `aria-expanded`, closes after route selection and does not cause horizontal page overflow.
- Dark/Light/System work on first visit and after reload; System follows OS preference. Keyboard focus and text contrast remain usable.
- Build stays self-contained for low-resource router targets; all static assets are present in the binary.

---

### Task 1: Add failing tests and serve embedded panel assets

**Files:**
- Create: `server/internal/app/panel_assets_test.go`
- Create: `server/internal/app/panel_assets.go`
- Create: `server/internal/app/assets/panel.css` (initially carry forward the current styling)
- Create: `server/internal/app/assets/theme.js` (minimal same-origin asset, behavior is Task 4)
- Modify: `server/internal/app/app.go`
- Modify: `server/internal/app/panel.go`
- Modify: `web/README.md`

**Interfaces:** Serve `GET /style.css` as `text/css; charset=utf-8` and `GET /theme.js` as `text/javascript; charset=utf-8`, both from `go:embed`, without authentication. Other methods return `405` or retain the existing route method behavior.

- [x] **Step 1:** Add `TestPanelStaticAssetRoutes` for status, content type and non-empty body from both paths; assert `/theme.js` is public. Run `go test ./internal/app -run TestPanelStaticAssetRoutes -count=1`; confirmed RED: unauthenticated `/theme.js` returned 303 to `/login`.
- [x] **Step 2:** Move the existing CSS into `assets/panel.css`, embed CSS/JS, register `/theme.js`, and serve both same-origin assets.
- [x] **Step 3:** Update `web/README.md` to describe the actual embedded asset location and run the focused test; PASS.

### Task 2: Build the shared accessible panel shell

**Files:**
- Modify: `server/internal/app/panel.go`
- Modify: `server/internal/app/auth.go` only if the login template needs the shared theme labels
- Create or modify: `server/internal/app/panel_assets_test.go`

**Interfaces:** Authenticated templates use one shell with a desktop sidebar, mobile menu toggle, theme selector and skip link. Login and registration expose theme selection but not authenticated links. Add localized RU/EN labels for theme modes and navigation controls.

- [x] **Step 1:** Add tests for the theme selector and local asset references on login, registration and an authenticated dashboard render. Focused `TestPanel` run confirmed RED on the missing selector/sidebar hooks.
- [x] **Step 2:** Implement semantic `aside`/`nav`/`main`, `data-nav-link` hooks for Task 4's `aria-current` assignment, labeled selector, `aria-controls`/`aria-expanded` menu button and skip link. Kept existing route links and form actions intact.
- [x] **Step 3:** Focused shell tests and fresh `go test ./internal/app -count=1` PASS.

### Task 3: Replace inline styling with responsive Mounvix design tokens

**Files:**
- Modify: `server/internal/app/assets/panel.css`
- Modify: `server/internal/app/panel.go` (wrap each data table in a horizontal-scroll container)
- Modify: `server/internal/app/panel_assets_test.go`

**Interfaces:** CSS defines semantic light/dark/system tokens, dark ink-blue surfaces and existing Mounvix blue accents, compact non-card metric summary, consistent forms/tables, visible focus, responsive sidebar and narrow-table overflow. Dark is the document default; System mode is resolved by `prefers-color-scheme`.

- [x] **Step 1:** Add `TestPanelCSSHasThemeAndResponsiveRules` for semantic tokens, system media query, focus state and mobile breakpoint. Confirmed RED on all missing markers.
- [x] **Step 2:** Implement dark/light/system tokens, Mounvix blue actions, consistent radius/spacing scales, accessible focus, responsive sidebar, metrics and forms; wrap all panel data tables in horizontal-scroll containers. Reduced-motion mode disables transitions to avoid a stuck hidden drawer. No gradients, glass or `transition: all`.
- [x] **Step 3:** Focused `TestPanel` and fresh `go test ./internal/app -count=1` PASS.

### Task 4: Implement theme persistence and responsive navigation behavior

**Files:**
- Modify: `server/internal/app/assets/theme.js`
- Modify: `server/internal/app/panel_assets_test.go`
- Modify: `server/internal/app/panel.go` (localized Settings title)
- Modify: `server/internal/app/feed_config_test.go` (RU/EN title regression coverage)

**Interfaces:** On first load, select Dark; accept only `dark`, `light` or `system` from localStorage. Update the root theme before stylesheet paint, synchronize the selector, persist changes, and implement the mobile menu's accessible open/close state without changing route behavior.

- [x] **Step 1:** Add `TestPanelScriptContainsThemeAndNavigationHooks` for the storage key, valid modes, system preference and aria-state wiring; confirmed RED against the placeholder script (including correction to the `dataset.open` hook).
- [x] **Step 2:** Implement vanilla-JS behavior with no third-party dependency and graceful storage failure handling; assign active nav state and provide mobile open/close, Escape, outside-click, resize and focus behavior. Reduced-motion CSS disables transitions so the drawer cannot remain at its hidden transition keyframe. Layer the drawer above the sticky topbar, add a localized touch-sized close button, make background/skip-link inert while open, and wrap keyboard focus within the drawer; closing restores focus to the toggle, and breakpoint changes move focus to a control visible at the new width.
- [x] **Step 3:** Focused Go tests and localized Settings regression passed; full Go tests/race/vet and six Linux target builds subsequently passed.

### Task 5: Full regression and visual acceptance

**Files:**
- Modify tests/docs only if verification exposes a defect.

- [x] **Step 1:** Fresh `go test -count=1 ./...`, `go test -race -count=1 ./...` and `go vet ./...` pass. `scripts/build-server.sh` cross-builds Linux arm64, arm, mipsle, mips, amd64 and 386.
- [x] **Step 2:** Start an isolated Windows preview at `http://127.0.0.1:48100/` with a temporary data directory; no router interaction.
- [x] **Step 3:** Playwright Extension QA covers login, registration, authenticated Overview/Settings/Folders, all 12 authenticated route GETs (200), representative forms/tables, 320/375/1280/1920 widths, RU/EN, Dark/Light/System and theme persistence. Mobile drawer visibility, active nav, first-link hit testing, localized close button, background/skip-link inertness, Tab/Shift+Tab wrapping, close paths, focus transfer/restore, breakpoint focus transitions and table containment verified; breakpoint changes were exercised with a controlled same-origin MediaQueryList harness. Extension native click/keypress timed out, so DOM click and synthetic-key fallbacks verified the handlers; physical pointer/keyboard interaction remains unverified.
- [x] **Step 4:** `git diff --cached --check` passes; inspected the final diff and review findings, verified preview/build artifacts are removed, and committed locally as `6e1c11d`. No Mounvix push/deploy.

## Plan self-review

- Existing route and data behavior remain untouched; shared template changes cover all authenticated panel pages, login and registration.
- Every new behavior has a focused Go test and browser acceptance state; visual verification uses the actual rendered panel rather than source-only assertions.
- The source screenshot, existing Mounvix navy/blue palette, low-resource Go server and approved sidebar/dark-first choices determine the visual direction.
