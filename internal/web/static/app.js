(() => {
  'use strict';
  const csrf = document.querySelector('meta[name="csrf-token"]')?.content;
  const active = new Set();
  const visualActive = new Set();
  const toast = message => {
    const el = document.querySelector('.toast');
    el.textContent = message;
    el.hidden = false;
    clearTimeout(el.timer);
    el.timer = setTimeout(() => { el.hidden = true; }, 9000);
  };
  const article = id => document.querySelector(`[data-article="${id}"]`);
  const elStatus = id => article(id)?.querySelector('.summary')?.dataset.status || '';
  function update(id, data) {
    const el = article(id);
    if (!el) return;
    const box = el.querySelector('.summary-content');
    const summary = el.querySelector('.summary');
    el.querySelector('.source-list')?.remove();
    summary.dataset.status = data.status;
    updateCost(el, 'summary', data.summary_cost);
    updateCost(el, 'infographic', data.infographic_cost);
    const hasPoints = Array.isArray(data.points) && data.points.length > 0;
    const hasTLDR = Array.isArray(data.tldr) && data.tldr.length > 0;
    const disclosure = el.querySelector('.summary-disclosure');
    disclosure.hidden = !hasPoints;
    el.querySelector('.tldr')?.remove();
    if (hasTLDR) {
      const link = document.createElement('div'); link.className = 'tldr';
      const label = document.createElement('span'); label.className = 'tldr-label'; label.textContent = 'TL;DR';
      const lines = document.createElement('ul');
      data.tldr.forEach(line => { const li = document.createElement('li'); li.textContent = line; lines.append(li); });
      link.append(label, lines); summary.before(link);
    }
    const busy = ['queued', 'processing'].includes(data.status);
    const children = [];
    if (hasPoints) {
      const list = document.createElement('ul');
      data.points.forEach(point => {
        const li = document.createElement('li'); li.textContent = point; list.append(li);
      });
      children.push(list);
      if (data.status === 'ready' && data.title) el.querySelector('.article-title a').firstChild.textContent = `${data.title} `;
      if (Array.isArray(data.sources) && data.sources.length) {
        const sources = document.createElement('p'); sources.className = 'source-list';
        sources.append(document.createTextNode('参照元：'));
        data.sources.forEach((source, index) => {
          let url;
          try { url = new URL(source); } catch { return; }
          if (!['http:', 'https:'].includes(url.protocol)) return;
          if (index) sources.append(document.createTextNode(' · '));
          const link = document.createElement('a'); link.href = url.href;
          link.target = '_blank'; link.rel = 'noopener noreferrer'; link.textContent = url.hostname;
          sources.append(link);
        });
        if (sources.querySelector('a')) box.insertAdjacentElement('afterend', sources);
      }
    }
    const statusBox = el.querySelector('.summary-status');
    statusBox.replaceChildren();
    if (data.status !== 'ready') {
      const text = document.createElement('p'); text.className = 'summary-message';
      text.textContent = busy
        ? `${data.progress || '記事の要点をまとめています'}…`
        : data.status === 'interrupted' ? '処理が中断されました。再試行できます。'
        : data.status === 'failed' ? (data.error || '要約を作成できませんでした。再試行できます。') : '要約はまだありません。作成できます。';
      statusBox.append(text);
    }
    box.replaceChildren(...children);
    const styles = el.querySelector('.summary-styles');
    if (styles) styles.hidden = !hasPoints;
    el.querySelectorAll('.summary-style, .summary-instruction').forEach(button => { button.disabled = busy; });
    const button = el.querySelector('.generate-button');
    if (button) {
      button.hidden = hasPoints;
      button.disabled = busy;
      button.textContent = busy ? '要約を作成中…' : data.status === 'pending' ? '要約を作成する' : '要約を再試行する';
    }
    updateInfographic(el, data, hasPoints);
  }
  function updateCost(el, kind, label) {
    const node = el.querySelector(`.${kind}-cost`);
    if (node) { node.textContent = label || ''; node.hidden = !label; }
  }
  function updateInfographic(el, data, hasPoints) {
    const panel = el.querySelector('.infographic-panel');
    if (!panel) return;
    panel.hidden = !hasPoints;
    const previous = panel.dataset.infographicStatus;
    const status = data.infographic_status || '';
    panel.dataset.infographicStatus = status;
    const button = panel.querySelector('.infographic-button');
    const figure = panel.querySelector('.infographic-figure');
    const message = panel.querySelector('.infographic-message');
    const busy = status === 'queued' || status === 'processing';
    if (button) {
      button.hidden = false;
      button.disabled = busy || data.status !== 'ready';
      button.textContent = busy ? '図解を作成中…' : status === 'ready' ? '絵を作り直す' : status === 'failed' || status === 'interrupted' ? '図解を再試行する' : '絵で説明する';
    }
    figure.hidden = status !== 'ready';
    if (status === 'ready' && previous !== 'ready') {
      const imageURL = `/articles/${el.dataset.article}/infographic/image?t=${Date.now()}`;
      figure.querySelector('img').src = imageURL;
      figure.querySelector('a').href = imageURL;
    }
    message.hidden = !busy && status !== 'failed' && status !== 'interrupted';
    message.textContent = busy ? '図解を作成しています。ページを離れても続きます。' : status === 'failed' ? (data.infographic_error || '図解を作成できませんでした。再試行できます。') : status === 'interrupted' ? '処理が中断されました。再試行できます。' : '';
  }
  async function pollInfographic(id) {
    for (let i = 0; article(id) && ['queued', 'processing'].includes(article(id).querySelector('.infographic-panel')?.dataset.infographicStatus); i++) {
      await new Promise(resolve => setTimeout(resolve, i < 20 ? 3000 : 15000));
      try {
        const response = await fetch(`/articles/${id}/summary`);
        if ([401, 403, 404].includes(response.status)) return;
        if (!response.ok) continue;
        const data = await response.json(); update(id, data);
        if (!['queued', 'processing'].includes(data.infographic_status)) return;
      } catch { /* Retry temporary network failures while this page is open. */ }
    }
  }
  async function generateInfographic(id) {
    if (!csrf || !/^[a-f0-9]{64}$/.test(id) || visualActive.has(id)) return;
    visualActive.add(id);
    const button = article(id)?.querySelector('.infographic-button');
    if (button) button.disabled = true;
    try {
      const response = await fetch(`/articles/${id}/infographic`, {
        method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ csrf }),
      });
      if (!response.headers.get('content-type')?.includes('application/json')) throw new Error('通信に失敗しました。時間をおいて再試行してください。');
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || '図解を作成できませんでした。');
      update(id, data);
      if (['queued', 'processing'].includes(data.infographic_status)) await pollInfographic(id);
    } catch (error) {
      toast(error.message);
      if (button) button.disabled = false;
    } finally {
      visualActive.delete(id);
    }
  }
  async function poll(id) {
    // Queue delays and retries can outlast a single worker attempt.
    for (let i = 0; article(id) && ['queued', 'processing'].includes(elStatus(id)); i++) {
      await new Promise(resolve => setTimeout(resolve, i < 20 ? 3000 : 15000));
      try {
        const response = await fetch(`/articles/${id}/summary`);
        if ([401, 403, 404].includes(response.status)) return;
        if (!response.ok) continue;
        const data = await response.json(); update(id, data);
        if (!['queued','processing'].includes(data.status)) return;
      } catch { /* Retry temporary network failures while this page is open. */ }
    }
  }
  async function generate(id, style = '') {
    if (!csrf || !/^[a-f0-9]{64}$/.test(id) || active.has(id)) return;
    const el = article(id);
    const input = el?.querySelector('.summary-instruction');
    const instruction = input?.value.trim() || '';
    if (input && !validateInstruction(input)) { input.reportValidity(); return; }
    active.add(id);
    const controls = el?.querySelectorAll('.generate-button, .summary-style, .summary-instruction') || [];
    controls.forEach(button => { button.disabled = true; });
    try {
      const response = await fetch(`/articles/${id}/summary`, {
        method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ csrf, style, instruction }),
      });
      if (!response.headers.get('content-type')?.includes('application/json')) {
        throw new Error(response.status === 401 || response.status === 403 ? 'ページを再読み込みし、ログインしてから再試行してください。' : '通信に失敗しました。時間をおいて再試行してください。');
      }
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || '要約を作成できませんでした。');
      if (input) input.value = '';
      update(id, data);
      if (['queued','processing'].includes(data.status)) await poll(id);
    } catch (error) {
      toast(error.message);
    } finally {
      active.delete(id);
      if (!['queued','processing'].includes(elStatus(id))) controls.forEach(button => { button.disabled = false; });
    }
  }
  document.querySelectorAll('[data-copy-target]').forEach(button => button.addEventListener('click', async () => {
    const target = document.getElementById(button.dataset.copyTarget);
    if (!target) return;
    const value = target instanceof HTMLInputElement ? target.value : target.textContent;
    try {
      await navigator.clipboard.writeText(value);
      toast('コピーしました。');
    } catch {
      if (target instanceof HTMLInputElement) {
        target.focus(); target.select();
      } else {
        const range = document.createRange(); range.selectNodeContents(target);
        const selection = window.getSelection(); selection.removeAllRanges(); selection.addRange(range);
      }
      toast('自動コピーできませんでした。選択した全文をコピーしてください。');
    }
  }));
  document.querySelectorAll('.generate-button').forEach(button => button.addEventListener('click', () => generate(button.dataset.id)));
  function collapseBookmark(el, collapsed) {
    if (!el) return;
    el.classList.toggle('is-collapsed', collapsed);
    const title = el.querySelector('.article-title a');
    if (el.classList.contains('is-understood')) {
      title.setAttribute('role', 'button');
      title.setAttribute('aria-expanded', String(!collapsed));
    } else {
      title.removeAttribute('role');
      title.removeAttribute('aria-expanded');
    }
    if (collapsed) title.focus();
  }
  document.querySelectorAll('.article-title a').forEach(title => {
    title.addEventListener('click', event => {
      const el = title.closest('.bookmark');
      if (!el.classList.contains('is-understood')) return;
      event.preventDefault();
      collapseBookmark(el, !el.classList.contains('is-collapsed'));
    });
    title.addEventListener('keydown', event => {
      if (event.key === ' ' && title.closest('.bookmark').classList.contains('is-understood')) {
        event.preventDefault();
        title.click();
      }
    });
  });
  document.querySelectorAll('.understood-button').forEach(button => button.addEventListener('click', async () => {
    const id = button.dataset.id;
    if (!csrf || button.disabled || !/^[a-f0-9]{64}$/.test(id)) return;
    const understood = button.getAttribute('aria-pressed') !== 'true';
    button.disabled = true;
    try {
      const response = await fetch(`/bookmarks/${id}/understood`, {
        method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ csrf, understood: String(understood) }),
      });
      if (!response.ok || !response.headers.get('content-type')?.includes('application/json')) {
        throw new Error(response.status === 401 || response.status === 403 ? 'ページを再読み込みし、ログインしてから再試行してください。' : '理解済みの状態を保存できませんでした。もう一度お試しください。');
      }
      const data = await response.json();
      button.setAttribute('aria-pressed', String(data.understood));
      button.textContent = data.understood ? '✓ 理解済み' : '理解した';
      const el = article(id);
      el?.classList.toggle('is-understood', data.understood);
      collapseBookmark(el, data.understood);
      if (data.understood && el?.closest('.feed')?.dataset.filter === 'unread') {
        // Refresh to fill this filtered page and update its pagination.
        window.location.reload();
      }
    } catch (error) {
      toast(error.message);
    } finally {
      button.disabled = false;
    }
  }));
  document.querySelectorAll('.rating-controls').forEach(group => {
    group.addEventListener('click', async event => {
      const button = event.target.closest('.rating-star, .rating-clear');
      if (!button || button.disabled || !csrf || group.dataset.busy === 'true') return;
      const id = group.dataset.id;
      if (!/^[a-f0-9]{64}$/.test(id)) return;
      const rating = button.classList.contains('rating-clear') ? 0 : Number(button.dataset.rating);
      group.dataset.busy = 'true';
      const buttons = group.querySelectorAll('button');
      buttons.forEach(control => { control.disabled = true; });
      try {
        const response = await fetch(`/bookmarks/${id}/rating`, {
          method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: new URLSearchParams({ csrf, rating: String(rating) }),
        });
        if (!response.ok || !response.headers.get('content-type')?.includes('application/json')) {
          throw new Error(response.status === 401 || response.status === 403 ? 'ページを再読み込みし、ログインしてから再試行してください。' : '評価を保存できませんでした。もう一度お試しください。');
        }
        const data = await response.json();
        group.dataset.rating = String(data.rating);
        group.querySelectorAll('.rating-star').forEach(star => {
          const value = Number(star.dataset.rating);
          star.setAttribute('aria-pressed', String(value === data.rating));
          star.dataset.filled = String(value <= data.rating);
          star.querySelector('span').textContent = value <= data.rating ? '⭐' : '☆';
        });
        article(id).querySelector('.rating-total-value').textContent = String(data.rating_total);
        if (new URLSearchParams(window.location.search).get('sort') === 'popular') window.location.reload();
      } catch (error) {
        toast(error.message);
      } finally {
        delete group.dataset.busy;
        buttons.forEach(control => { control.disabled = false; });
        group.querySelector('.rating-clear').disabled = Number(group.dataset.rating) === 0;
      }
    });
  });
  function validateInstruction(input) {
    const valid = [...input.value.trim()].length <= Number(input.dataset.maxlength);
    const error = input.closest('form').querySelector('.summary-custom-error');
    const message = valid ? '' : '追加指示は200文字以内で入力してください。';
    input.setCustomValidity(message);
    input.setAttribute('aria-invalid', String(!valid));
    error.textContent = message;
    error.hidden = valid;
    return valid;
  }
  document.querySelectorAll('.summary-instruction').forEach(input => input.addEventListener('input', () => validateInstruction(input)));
  document.querySelectorAll('.summary-custom').forEach(form => form.addEventListener('submit', event => {
    event.preventDefault();
    generate(form.dataset.id, 'concrete');
  }));
  document.querySelectorAll('.summary-style[data-style]').forEach(button => button.addEventListener('click', () => generate(button.dataset.id, button.dataset.style)));
  document.querySelectorAll('.summary[data-status="queued"], .summary[data-status="processing"]').forEach(el => poll(el.closest('[data-article]').dataset.article));
  document.querySelectorAll('.infographic-button').forEach(button => button.addEventListener('click', () => generateInfographic(button.dataset.id)));
  document.querySelectorAll('.infographic-panel[data-infographic-status="queued"], .infographic-panel[data-infographic-status="processing"]').forEach(el => pollInfographic(el.closest('[data-article]').dataset.article));
  document.querySelectorAll('.delete-form').forEach(form => form.addEventListener('submit', event => {
    if (!window.confirm('このブックマークを削除しますか？')) event.preventDefault();
  }));
})();
