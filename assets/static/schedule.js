(() => {
  const table = document.getElementById('schedule');
  if (!table) return;
  const csrf = table.dataset.csrf;

  async function post(url, params) {
    params.set('csrf', csrf);
    const res = await fetch(url, { method: 'POST', body: params });
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

  function dayBoxes(row) {
    return row.querySelectorAll('.edit-days input[type="checkbox"]');
  }

  function cancelEdit(row) {
    row.querySelector('.edit-name').value = row.querySelector('.view-name').textContent;
    row.querySelector('.edit-start').value = row.querySelector('.view-start').textContent;
    row.querySelector('.edit-end').value = row.querySelector('.view-end').textContent;
    dayBoxes(row).forEach((b) => (b.checked = b.defaultChecked));
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
      const params = new URLSearchParams();
      params.set('id', id);
      params.set('name', row.querySelector('.edit-name').value.trim());
      params.set('start', row.querySelector('.edit-start').value);
      params.set('end', row.querySelector('.edit-end').value);
      dayBoxes(row).forEach((b) => b.checked && params.append('weekdays', b.value));
      withBusy(row, async () => {
        const p = await post('/admin/schedule/update', params);
        row.querySelector('.view-name').textContent = p.name;
        row.querySelector('.view-days').textContent = p.days;
        row.querySelector('.view-start').textContent = p.start;
        row.querySelector('.view-end').textContent = p.end;
        row.querySelector('.edit-name').value = p.name;
        row.querySelector('.edit-start').value = p.start;
        row.querySelector('.edit-end').value = p.end;
        dayBoxes(row).forEach((b) => (b.defaultChecked = b.checked));
        row.classList.remove('editing');
      });
    }
  });
})();
