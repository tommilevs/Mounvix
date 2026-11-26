(() => {
  'use strict';
  const installationsRoot = document.querySelector('#plugin-installations');
  const jobsRoot = document.querySelector('#plugin-jobs');
  const syncForm = document.querySelector('#rclone-sync-form');
  if (!installationsRoot && !jobsRoot && !syncForm) return;

  const sourceAPI = installationsRoot?.dataset.sourceApi;
  const installationsAPI = installationsRoot?.dataset.apiUrl;
  const make = (tag, text, className) => {
    const node = document.createElement(tag);
    if (text !== undefined) node.textContent = String(text);
    if (className) node.className = className;
    return node;
  };
  const request = async (url, options = {}) => {
    const response = await fetch(url, { credentials: 'same-origin', cache: 'no-store', ...options });
    if (!response.ok) {
      let message = 'Запрос не выполнен.';
      try { const body = await response.json(); message = body.error?.message || body.summary || message; } catch (_) { /* Keep generic message. */ }
      throw new Error(message);
    }
    if (response.status === 204) return null;
    return response.json();
  };
  const idempotencyKey = () => {
    if (window.crypto?.randomUUID) return window.crypto.randomUUID();
    const bytes = new Uint8Array(16);
    window.crypto.getRandomValues(bytes);
    return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
  };
  const setFormStatus = (form, message) => {
    const node = form?.querySelector('[data-form-status]');
    if (node) node.textContent = message;
  };

  const renderSourceControls = (item) => {
    const sources = [
      ['package', item.package_present, item.package_enabled],
      ['legacy', item.legacy_present, item.legacy_enabled],
      ['builtin', item.builtin_present, item.builtin_enabled],
    ];
    const wrapper = make('div');
    for (const [source, present, enabled] of sources) {
      if (!present) continue;
      const button = make('button', `${source}: ${enabled ? 'включён' : 'выключен'}`);
      button.type = 'button';
      button.dataset.toggleSource = source;
      button.dataset.pluginId = item.id;
      button.dataset.enabled = String(enabled);
      button.addEventListener('click', async () => {
        button.disabled = true;
        try {
          await request(`${sourceAPI}/${encodeURIComponent(item.id)}`, {
            method: 'PATCH', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ source, enabled: button.dataset.enabled !== 'true' }),
          });
          await loadInstallations();
        } catch (error) {
          button.disabled = false;
          button.textContent = error.message;
        }
      });
      wrapper.append(button, document.createTextNode(' '));
    }
    return wrapper;
  };
  const renderInstallation = (item) => {
    const card = make('article', undefined, 'installation-card');
    const title = make('h3', `${item.name} (${item.id})`);
    const detail = make('p', `Эффективный источник: ${item.effective_source || 'нет'} · ${item.package_present ? `пакет ${item.package_version || 'установлен'}` : 'пакет отсутствует'} · ${item.available ? 'доступен' : 'недоступен'}`);
    card.append(title, detail, renderSourceControls(item));
    if (item.legacy_fallback_enabled) card.append(make('p', 'При удалении пакета включённый legacy-вариант станет активным.', 'status-note'));
    if (item.pending_operation) {
      card.append(make('p', `Операция ${item.pending_operation.operation}: ${item.pending_operation.status} (${item.pending_operation.run_id})`, 'status-note'));
    }
    if (item.package_present && !item.package_enabled && !item.pending_operation) {
      const removeForm = make('form', undefined, 'row');
      removeForm.addEventListener('submit', async (event) => {
        event.preventDefault();
        const checkbox = removeForm.querySelector('[name="confirmed_legacy_fallback"]');
        const confirmed = checkbox ? checkbox.checked : false;
        const button = removeForm.querySelector('button');
        button.disabled = true;
        try {
          const response = await fetch(`${installationsAPI}/${encodeURIComponent(item.id)}`, {
            method: 'DELETE', credentials: 'same-origin', cache: 'no-store',
            headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey() },
            body: JSON.stringify({ confirmed_legacy_fallback: confirmed }),
          });
          if (response.status === 204) {
            card.replaceChildren(make('p', 'Пакет уже отсутствует.', 'status-note'));
          } else if (response.ok) {
            card.append(make('p', 'Удаление добавлено в журнал заданий.', 'status-note'));
            await loadInstallations();
          } else {
            const payload = await response.json().catch(() => ({}));
            throw new Error(payload.error?.message || 'Удаление не принято.');
          }
        } catch (error) {
          button.disabled = false;
          card.append(make('p', error.message, 'err'));
        }
      });
      if (item.legacy_fallback_enabled) {
        const label = make('label');
        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.name = 'confirmed_legacy_fallback';
        checkbox.required = true;
        label.append(checkbox, document.createTextNode(' Подтверждаю активацию legacy-версии после удаления'));
        removeForm.append(label);
      }
      const removeButton = make('button', 'Удалить пакет');
      removeButton.type = 'submit';
      removeForm.append(removeButton);
      card.append(removeForm);
    }
    return card;
  };
  const loadInstallations = async () => {
    if (!installationsRoot || !installationsAPI) return;
    try {
      const data = await request(installationsAPI);
      const fragment = document.createDocumentFragment();
      if (!data.items?.length) fragment.append(make('p', 'Установленных пакетных плагинов нет.', 'muted'));
      for (const item of data.items || []) fragment.append(renderInstallation(item));
      installationsRoot.replaceChildren(fragment);
    } catch (error) { installationsRoot.replaceChildren(make('p', error.message, 'err')); }
  };

  const renderJob = (job) => {
    const item = make('li', undefined, 'job-item');
    item.append(make('strong', `${job.kind} · ${job.operation || job.job_id || job.plugin_id || 'job'}`));
    item.append(make('span', ` — ${job.status} · ${job.run_id}`));
    if (job.summary) item.append(make('p', job.summary, 'muted'));
    if (['queued', 'running'].includes(job.status)) {
      const cancel = make('button', 'Отменить');
      cancel.type = 'button';
      cancel.addEventListener('click', async () => {
        cancel.disabled = true;
        try {
          await request(`${jobsRoot.dataset.apiUrl}/${encodeURIComponent(job.run_id)}/cancellations`, { method: 'POST' });
          await loadJobs();
        } catch (error) { cancel.disabled = false; item.append(make('p', error.message, 'err')); }
      });
      item.append(cancel);
    }
    if (job.kind === 'plugin' || job.kind === 'rclone') {
      const logs = make('button', 'Показать журнал запуска');
      logs.type = 'button';
      logs.addEventListener('click', async () => {
        logs.disabled = true;
        try {
          const data = await request(`${jobsRoot.dataset.apiUrl}/${encodeURIComponent(job.run_id)}/logs`);
          const pre = make('pre', data.log || 'Журнал пуст.');
          item.append(pre);
        } catch (error) { item.append(make('p', error.message, 'muted')); }
        finally { logs.disabled = false; }
      });
      item.append(logs);
    }
    return item;
  };
  const loadJobs = async () => {
    if (!jobsRoot || !jobsRoot.dataset.apiUrl) return;
    try {
      const url = new URL(jobsRoot.dataset.apiUrl, window.location.href);
      url.searchParams.set('limit', '10');
      const page = await request(url);
      const fragment = document.createDocumentFragment();
      const jobs = (page.items || []).slice().reverse();
      if (!jobs.length) fragment.append(make('p', 'Заданий пока нет.', 'muted'));
      else {
        const list = document.createElement('ol');
        for (const job of jobs) list.append(renderJob(job));
        fragment.append(list);
      }
      jobsRoot.replaceChildren(fragment);
    } catch (error) { jobsRoot.replaceChildren(make('p', error.message, 'err')); }
  };

  if (syncForm) {
    const remoteSelect = syncForm.querySelector('[name="remote_name"]');
    const remotesURL = remoteSelect?.dataset.remotesUrl;
    if (remotesURL) {
      void request(remotesURL).then((data) => {
        remoteSelect.replaceChildren(make('option', 'Выберите хранилище'));
        remoteSelect.options[0].value = '';
        for (const remote of data.remotes || []) {
          const option = make('option', remote);
          option.value = remote;
          remoteSelect.append(option);
        }
        if (!data.remotes?.length) setFormStatus(syncForm, 'В конфигурации нет доступных хранилищ.');
      }).catch((error) => setFormStatus(syncForm, error.message));
    }
    syncForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      const values = new FormData(syncForm);
      const payload = {
        source_path: String(values.get('source_path') || ''),
        remote_name: String(values.get('remote_name') || ''),
        destination_path: String(values.get('destination_path') || ''),
        confirmed: values.get('confirmed') === 'on',
      };
      const button = syncForm.querySelector('button[type="submit"]');
      button.disabled = true;
      try {
        const data = await request(syncForm.dataset.apiUrl, {
          method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey() },
          body: JSON.stringify(payload),
        });
        setFormStatus(syncForm, `Задание принято: ${data.job?.run_id || 'run id недоступен'}`);
        syncForm.reset();
        await loadJobs();
      } catch (error) { setFormStatus(syncForm, error.message); }
      finally { button.disabled = false; }
    });
  }

  void loadInstallations();
  void loadJobs();
  window.addEventListener('mounvix:notification', () => { void loadJobs(); });
})();
