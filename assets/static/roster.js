(() => {
  const table = document.getElementById('roster');
  if (!table) return;
  const csrf = table.dataset.csrf;
  const includeInactive = table.dataset.includeInactive === '1';

  async function post(url, data) {
    const body = new URLSearchParams({ csrf, ...data });
    const res = await fetch(url, { method: 'POST', body });
    let payload = {};
    try { payload = await res.json(); } catch (_) {}
    if (!res.ok) throw new Error(payload.error || 'Request failed');
    return payload;
  }

  async function withBusy(row, fn) {
    row.querySelectorAll('button').forEach((b) => (b.disabled = true));
    try {
      await fn();
    } catch (err) {
      showError(row, err.message);
    } finally {
      row.querySelectorAll('button').forEach((b) => (b.disabled = false));
    }
  }

  function showError(row, text) {
    const el = row.querySelector('.row-msg');
    if (el) el.textContent = text;
  }

  function clearError(row) {
    const el = row.querySelector('.row-msg');
    if (el) el.textContent = '';
  }

  function setActiveUI(row, active) {
    row.classList.toggle('inactive', !active);
    row.querySelector('.view-status').textContent = active ? 'Active' : 'Inactive';
    row.querySelector('.act-deactivate').classList.toggle('hidden', !active);
    row.querySelector('.act-reactivate').classList.toggle('hidden', active);
  }

  function cancelEdit(row) {
    row.querySelector('.edit-name').value = row.querySelector('.view-name').textContent;
    row.querySelector('.edit-category').value = row.querySelector('.view-category').textContent;
    row.classList.remove('editing');
    clearError(row);
  }

  table.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    const row = e.target.closest('tr.editing');
    if (row) cancelEdit(row);
  });

  table.addEventListener('click', (e) => {
    const btn = e.target.closest('button');
    if (!btn) return;
    const row = btn.closest('tr');
    if (!row) return;
    const id = row.dataset.id;

    if (btn.classList.contains('act-edit')) {
      clearError(row);
      row.classList.add('editing');
      row.querySelector('.edit-name').focus();
    } else if (btn.classList.contains('act-save')) {
      const name = row.querySelector('.edit-name').value.trim();
      const category = row.querySelector('.edit-category').value.trim();
      withBusy(row, async () => {
        const p = await post('/admin/roster/update', { id, name, category });
        row.querySelector('.view-name').textContent = p.name;
        row.querySelector('.view-category').textContent = p.category;
        row.querySelector('.edit-name').value = p.name;
        row.querySelector('.edit-category').value = p.category;
        row.classList.remove('editing');
      });
    } else if (btn.classList.contains('act-deactivate')) {
      withBusy(row, async () => {
        await post('/admin/roster/deactivate', { id });
        if (includeInactive) setActiveUI(row, false);
        else row.remove();
      });
    } else if (btn.classList.contains('act-reactivate')) {
      withBusy(row, async () => {
        await post('/admin/roster/reactivate', { id });
        setActiveUI(row, true);
      });
    } else if (btn.classList.contains('act-delete')) {
      const name = row.querySelector('.view-name').textContent;
      if (!confirm(`Permanently delete ${name} and all their history? This cannot be undone.`)) return;
      withBusy(row, async () => {
        await post('/admin/roster/purge', { id });
        row.remove();
      });
    }
  });
})();
