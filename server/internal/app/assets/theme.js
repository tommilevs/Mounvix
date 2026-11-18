(() => {
  const root = document.documentElement;
  const storageKey = "mounvix_theme";
  const themes = new Set(["dark", "light", "system"]);

  // Set this before the stylesheet loads so compact navigation can enhance safely.
  root.dataset.js = "true";

  let theme = "dark";
  try {
    const savedTheme = window.localStorage.getItem(storageKey);
    if (themes.has(savedTheme)) theme = savedTheme;
  } catch (_) {
    // Storage may be blocked; keep the dark default and allow in-page switching.
  }
  root.dataset.theme = theme;

  const setTheme = (nextTheme, select) => {
    if (!themes.has(nextTheme)) return;
    theme = nextTheme;
    root.dataset.theme = theme;
    if (select) select.value = theme;
    try {
      window.localStorage.setItem(storageKey, theme);
    } catch (_) {
      // Theme still applies for this page when persistent storage is unavailable.
    }
  };

  const normalizedPath = (path) => (path.length > 1 ? path.replace(/\/+$/, "") : path);

  const initializePanel = () => {
    const select = document.getElementById("theme-select");
    if (select) {
      select.value = theme;
      select.addEventListener("change", () => setTheme(select.value, select));
    }

    const currentPath = normalizedPath(window.location.pathname);
    document.querySelectorAll("[data-nav-link]").forEach((link) => {
      const linkPath = normalizedPath(new URL(link.href, window.location.href).pathname);
      if (linkPath === currentPath) {
        link.setAttribute("aria-current", "page");
      } else {
        link.removeAttribute("aria-current");
      }
    });

    const sidebar = document.getElementById("sidebar");
    const toggle = document.getElementById("nav-toggle");
    const closeButton = document.getElementById("nav-close");
    const backgroundShell = document.querySelector(".main-shell");
    const skipLink = document.querySelector(".skip-link");
    if (!sidebar || !toggle) return;

    const compact = typeof window.matchMedia === "function"
      ? window.matchMedia("(max-width: 880px)")
      : null;
    const isCompact = () => compact ? compact.matches : window.innerWidth <= 880;

    const focusableSelector = [
      "a[href]",
      "button:not([disabled])",
      'input:not([disabled]):not([type="hidden"])',
      "select:not([disabled])",
      "textarea:not([disabled])",
      '[tabindex]:not([tabindex="-1"])',
    ].join(",");

    const setMenuOpen = (open, focusFirst = false) => {
      sidebar.dataset.open = String(open);
      toggle.setAttribute("aria-expanded", String(open));
      if (backgroundShell) backgroundShell.inert = open;
      if (skipLink) skipLink.inert = open;
      if (open && focusFirst) {
        const firstLink = sidebar.querySelector("a[data-nav-link]");
        if (firstLink) firstLink.focus();
      }
    };

    toggle.addEventListener("click", () => {
      const open = toggle.getAttribute("aria-expanded") !== "true";
      setMenuOpen(open, open);
    });

    if (closeButton) {
      closeButton.addEventListener("click", () => {
        setMenuOpen(false);
        toggle.focus();
      });
    }

    sidebar.querySelectorAll("a[data-nav-link]").forEach((link) => {
      link.addEventListener("click", () => {
        if (isCompact()) setMenuOpen(false);
      });
    });

    document.addEventListener("keydown", (event) => {
      const open = isCompact() && toggle.getAttribute("aria-expanded") === "true";
      if (!open) return;

      if (event.key === "Escape") {
        setMenuOpen(false);
        toggle.focus();
        return;
      }

      if (event.key === "Tab") {
        const focusable = Array.from(sidebar.querySelectorAll(focusableSelector))
          .filter((element) => element.getClientRects().length > 0);
        if (focusable.length === 0) {
          event.preventDefault();
          if (closeButton) closeButton.focus();
          return;
        }

        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && (document.activeElement === first || !sidebar.contains(document.activeElement))) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && (document.activeElement === last || !sidebar.contains(document.activeElement))) {
          event.preventDefault();
          first.focus();
        }
      }
    });

    document.addEventListener("click", (event) => {
      const target = event.target;
      if (
        isCompact()
        && toggle.getAttribute("aria-expanded") === "true"
        && target
        && !sidebar.contains(target)
        && !toggle.contains(target)
      ) {
        setMenuOpen(false);
        toggle.focus();
      }
    });

    const focusAfterResize = (activeElement) => {
      if (isCompact() && sidebar.contains(activeElement)) {
        toggle.focus();
        return;
      }

      if (!isCompact() && (toggle.contains(activeElement) || (closeButton && closeButton.contains(activeElement)))) {
        const firstLink = sidebar.querySelector("a[data-nav-link]");
        if (firstLink) firstLink.focus();
      }
    };

    const closeOnResize = () => {
      const activeElement = document.activeElement;
      setMenuOpen(false);
      focusAfterResize(activeElement);
    };
    if (compact && typeof compact.addEventListener === "function") {
      compact.addEventListener("change", closeOnResize);
    } else if (compact && typeof compact.addListener === "function") {
      compact.addListener(closeOnResize);
    }
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initializePanel, { once: true });
  } else {
    initializePanel();
  }
})();
