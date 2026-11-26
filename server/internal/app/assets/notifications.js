(() => {
  'use strict';

  const nav = document.querySelector('[data-notifications-api]');
  if (!nav) return;
  const apiURL = nav.dataset.notificationsApi;
  const wsURL = nav.dataset.notificationsWs;
  const root = document.querySelector('#notifications');
  const list = root?.querySelector('#notification-list');
  const status = root?.querySelector('#notification-status');
  const unreadNode = document.querySelector('#notification-unread-count');
  const badge = nav.querySelector('.unread-badge');
  let cursor = Number(root?.dataset.latestSeq || 0);
  let oldest = Number(root?.dataset.oldestSeq || 0);
  let unreadCount = Number(unreadNode?.textContent || badge?.textContent || 0);
  let socket = null;
  let retryTimer = null;
  let retryDelay = 500;
  let stopped = false;
  let fetching = false;
  const seen = new Set(Array.from(list?.querySelectorAll('[data-event-id]') || []).map((item) => item.dataset.eventId));

  const setStatus = (message) => { if (status) status.textContent = message; };
  const setUnread = (count) => {
    unreadCount = Math.max(0, Number(count) || 0);
    if (unreadNode) unreadNode.textContent = String(unreadCount);
    if (badge) {
      badge.textContent = String(unreadCount);
      badge.hidden = unreadCount === 0;
    } else if (unreadCount > 0) {
      const created = document.createElement('span');
      created.className = 'unread-badge';
      created.setAttribute('aria-label', `${unreadCount} unread`);
      created.textContent = String(unreadCount);
      nav.append(' ', created);
    }
  };
  const safeRoute = (value) => {
    try {
      const target = new URL(value, window.location.href);
      return target.origin === window.location.origin && target.pathname.startsWith('/') ? target.href : null;
    } catch (_) { return null; }
  };
  const removeEmptyState = () => list?.querySelector('[data-empty-state]')?.remove();
  const renderEvent = (event, prepend) => {
    if (!list || !event || !event.event_id || seen.has(event.event_id)) return;
    seen.add(event.event_id);
    removeEmptyState();
    const item = document.createElement('li');
    item.className = 'notification-item';
    item.dataset.eventId = event.event_id;
    item.dataset.seq = String(event.seq || '');
    const summary = document.createElement('span');
    summary.textContent = String(event.summary || '');
    item.append(summary);
    if (event.created_at) {
      const time = document.createElement('time');
      const date = new Date(event.created_at);
      time.dateTime = date.toISOString();
      time.textContent = date.toLocaleString();
      item.append(' ', time);
    }
    const route = safeRoute(event.target_route);
    if (route) {
      const link = document.createElement('a');
      link.href = route;
      link.textContent = 'Открыть';
      item.append(' ', link);
    }
    if (!event.is_read) {
      const button = document.createElement('button');
      button.type = 'button';
      button.dataset.readEvent = event.event_id;
      button.textContent = 'Отметить прочитанным';
      item.append(' ', button);
    }
    if (prepend) list.prepend(item); else list.append(item);
    window.dispatchEvent(new CustomEvent('mounvix:notification', { detail: event }));
  };
  const requestPage = async (params) => {
    const url = new URL(apiURL, window.location.href);
    Object.entries(params).forEach(([key, value]) => url.searchParams.set(key, String(value)));
    const response = await fetch(url, { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } });
    if (response.status === 401 || response.status === 403) { stopped = true; closeSocket(); return null; }
    if (!response.ok) throw new Error('Уведомления временно недоступны.');
    return response.json();
  };
  const catchUp = async () => {
    if (fetching || stopped || document.visibilityState !== 'visible') return;
    fetching = true;
    try {
      const page = await requestPage({ after: cursor, limit: 50 });
      if (!page) return;
      if (page.reset && root) {
        root.dataset.reset = 'true';
        const note = document.createElement('p');
        note.className = 'status-note';
        note.textContent = 'Часть старой истории больше не хранится; список обновлён.';
        root.prepend(note);
      }
      for (const event of page.items || []) renderEvent(event, true);
      cursor = Math.max(cursor, Number(page.latest_seq) || 0);
      oldest = oldest || Number(page.oldest_seq) || 0;
      setUnread(page.unread_count);
      if (root) root.dataset.latestSeq = String(cursor);
      setStatus('');
    } catch (error) {
      setStatus(error.message || 'Уведомления временно недоступны.');
    } finally {
      fetching = false;
    }
  };
  const refreshCount = async () => {
    try {
      const page = await requestPage({ limit: 1 });
      if (page) {
        cursor = Math.max(cursor, Number(page.latest_seq) || 0);
        setUnread(page.unread_count);
      }
    } catch (_) { /* Keep the server-rendered badge when the inbox is unavailable. */ }
  };
  const loadOlder = async () => {
    if (!root || !oldest) return;
    try {
      const url = new URL(apiURL, window.location.href);
      url.searchParams.set('before', String(oldest));
      url.searchParams.set('limit', '50');
      const response = await fetch(url, { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error('Не удалось загрузить старые уведомления.');
      const page = await response.json();
      if (page.reset) {
        root.dataset.reset = 'true';
        setStatus('Часть старой истории больше не хранится.');
      }
      for (const event of page.items || []) renderEvent(event, false);
      oldest = Number(page.before_cursor) || oldest;
      root.dataset.oldestSeq = String(oldest);
      if (!page.items?.length || oldest <= Number(page.oldest_seq)) {
        const button = root.querySelector('#notification-load-older');
        if (button) button.disabled = true;
      }
    } catch (error) { setStatus(error.message || 'Не удалось загрузить старые уведомления.'); }
  };
  const closeSocket = () => {
    if (retryTimer) { window.clearTimeout(retryTimer); retryTimer = null; }
    if (socket) {
      const current = socket;
      socket = null;
      current.onclose = null;
      current.close();
    }
  };
  const connect = () => {
    if (stopped || document.visibilityState !== 'visible' || socket || !wsURL) return;
    const address = new URL(wsURL, window.location.href);
    address.protocol = address.protocol === 'https:' ? 'wss:' : 'ws:';
    try { socket = new WebSocket(address); } catch (_) { scheduleReconnect(); return; }
    socket.onopen = () => { retryDelay = 500; void catchUp(); };
    socket.onmessage = (message) => {
      try {
        const event = JSON.parse(message.data);
        if (event.type === 'notification') void catchUp();
      } catch (_) { /* Ignore malformed event frames; REST remains authoritative. */ }
    };
    socket.onerror = () => socket?.close();
    socket.onclose = () => { socket = null; scheduleReconnect(); };
  };
  const scheduleReconnect = () => {
    if (stopped || document.visibilityState !== 'visible' || retryTimer) return;
    retryTimer = window.setTimeout(() => {
      retryTimer = null;
      connect();
    }, retryDelay);
    retryDelay = Math.min(retryDelay * 2, 30000);
  };

  document.addEventListener('click', async (event) => {
    const readButton = event.target.closest('[data-read-event]');
    if (readButton) {
      readButton.disabled = true;
      try {
        const url = `${apiURL}/${encodeURIComponent(readButton.dataset.readEvent)}/read`;
        const response = await fetch(url, { method: 'POST', credentials: 'same-origin', cache: 'no-store' });
        if (!response.ok && response.status !== 204) throw new Error('Не удалось отметить уведомление.');
        readButton.closest('[data-event-id]')?.classList.add('is-read');
        readButton.remove();
        setUnread(unreadCount - 1);
      } catch (error) {
        readButton.disabled = false;
        setStatus(error.message || 'Не удалось отметить уведомление.');
      }
    }
    if (event.target.closest('#notification-load-older')) void loadOlder();
  });
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') { void catchUp(); connect(); }
    else closeSocket();
  });
  window.addEventListener('pagehide', () => { stopped = true; closeSocket(); });

  void refreshCount().then(() => {
    if (root) void catchUp();
    connect();
  });
})();
