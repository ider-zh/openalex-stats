(function (root) {
  'use strict';
  const STORAGE_KEY = 'openalex-atlas:onboarding:v1';
  const COOLDOWN_MS = 7 * 24 * 60 * 60 * 1000;
  function shouldShow(raw, now) {
    try {
      const record = JSON.parse(raw);
      const at = record?.dismissedAt;
      return !(record?.version === 1 && typeof at === 'number' && Number.isFinite(at) && at > 0 && at <= now && now - at < COOLDOWN_MS);
    } catch (_) { return true; }
  }
  function createGate(storage, now = Date.now) {
    let attempted = false;
    return {
      eligible() {
        if (attempted) return false;
        try { return shouldShow(storage()?.getItem(STORAGE_KEY), now()); }
        catch (_) { return true; }
      },
      started() { attempted = true; },
      dismiss() {
        attempted = true;
        try { storage()?.setItem(STORAGE_KEY, JSON.stringify({ version: 1, dismissedAt: now() })); }
        catch (_) { /* The current-page gate still prevents repeated prompts. */ }
      }
    };
  }
  // The same policy runs in the browser and in clock/storage unit tests.
  if (!root.document) {
    root.OpenAlexOnboardingPolicy = { STORAGE_KEY, COOLDOWN_MS, shouldShow, createGate };
    return;
  }
  const doc = root.document;
  const gate = createGate(() => root.localStorage);
  let panel, marker, help, active = false, index = 0, previousFocus, pendingFrame;
  let ready = false;
  const steps = [
    { title: '先确定数据范围', selector: '#corpusButtons', text: 'Core 是主数据；“含 XPAC 全数据”纳入扩展文献。两版独立统计，切换后用同一版本比较国家、年份和学科。' },
    { title: '选择分类与多个学科', selector: '#disciplinePicker', text: 'Topics 可选 Field / Subfield，Concepts 可选 Level 0 / 1。先定规范与层级，再打开学科选择器搜索、多选同层学科；数量类曲线会组合国家与学科。' },
    { title: '搜索国家，展开全球视野', selector: '.countries-block .search-box', text: '国家支持中文、英文和 ISO 搜索，也可批量选择。国家 ECI 的全球排名 Bump 可以容纳全部国家；排名始终基于全部有效国家，显示选择不会重新排名。' },
    { title: '从问题选择指标', selector: '.metric-groups', text: '“数量与引用”看论文量、当年被引与全球份额；RCA 看比较优势。“知识复杂度”里，ECI 比较国家的整体学科组合，PCI 比较学科本身。' },
    { title: '用两种方法阅读复杂度', selector: '#methodWrap', fallback: '[data-metric="eci"]', text: '国家 ECI 和学科 PCI 默认使用小倪方法论，也可切换谱方法，并用全球排名 Bump 或分数折线查看。两种方法的排名可能不同；方法切换只影响知识复杂度。' },
    { title: '读曲线，也保留数据', selector: '.chart-heading', text: '鼠标悬浮可看当年数值与排序，点击图例可切换曲线显示；“下载曲线 CSV”保存当前选择。年份从 2000 年起，2026 是截至快照日期的未完整年度，比较时请留意。' }
  ];
  function visible(element) { return !!element && !element.closest('[hidden]') && element.getClientRects().length > 0; }
  function usable() {
    return visible(doc.getElementById('workspace')) && !visible(doc.getElementById('notReady')) && !visible(doc.getElementById('chartError'));
  }
  function target() {
    const step = steps[index], element = doc.querySelector(step.selector);
    return visible(element) ? element : step.fallback && visible(doc.querySelector(step.fallback)) ? doc.querySelector(step.fallback) : null;
  }
  function button(text, action, className) {
    const element = doc.createElement('button');
    element.type = 'button'; element.textContent = text; element.className = className || '';
    element.addEventListener('click', action); return element;
  }
  function mount() {
    if (help || !doc.body) return;
    const host = doc.querySelector('.header-meta') || doc.querySelector('.masthead');
    if (!host) return;
    help = button('使用导引', start, 'oa-guide-help');
    help.disabled = !ready; help.title = '重看使用导引'; host.append(help);
    if (doc.getElementById('workspace')?.dataset.ready === 'true') onReady();
  }
  function position() {
    pendingFrame = null;
    if (!active) return;
    const item = target(), margin = 12, vw = doc.documentElement.clientWidth, vh = root.innerHeight;
    panel.style.maxHeight = `${Math.max(140, vh - margin * 2)}px`;
    const box = panel.getBoundingClientRect(), rect = item?.getBoundingClientRect();
    let left = (vw - box.width) / 2, top = (vh - box.height) / 2;
    if (rect) {
      const clipped = { left: Math.max(5, rect.left - 5), top: Math.max(5, rect.top - 5), right: Math.min(vw - 5, rect.right + 5), bottom: Math.min(vh - 5, rect.bottom + 5) };
      marker.hidden = clipped.right <= clipped.left || clipped.bottom <= clipped.top;
      Object.assign(marker.style, { left: `${clipped.left}px`, top: `${clipped.top}px`, width: `${Math.max(0, clipped.right - clipped.left)}px`, height: `${Math.max(0, clipped.bottom - clipped.top)}px` });
      if (vw > 700 && rect.right + box.width + 28 < vw) {
        left = rect.right + 18; top = rect.top;
      } else if (rect.top > box.height + 24) {
        left = rect.left; top = rect.top - box.height - 16;
      } else {
        left = rect.left; top = rect.bottom + 16;
        if (top + box.height > vh - margin) top = vh - box.height - margin;
      }
    } else marker.hidden = true;
    panel.style.left = `${Math.max(margin, Math.min(left, vw - box.width - margin))}px`;
    panel.style.top = `${Math.max(margin, Math.min(top, vh - box.height - margin))}px`;
  }
  function schedulePosition() { if (active && !pendingFrame) pendingFrame = root.requestAnimationFrame(position); }
  function render() {
    const step = steps[index];
    panel.querySelector('.oa-guide-progress').textContent = `使用导引 · ${index + 1} / ${steps.length}`;
    panel.querySelector('#oa-guide-title').textContent = step.title;
    panel.querySelector('#oa-guide-description').textContent = step.text;
    panel.querySelector('.oa-guide-back').disabled = index === 0;
    panel.querySelector('.oa-guide-next').textContent = index === steps.length - 1 ? '完成导引' : '下一步 →';
    panel.dataset.step = String(index + 1);
    const item = target();
    if (item) {
      const rect = item.getBoundingClientRect();
      // Keep the explained control above the mobile card without altering filters.
      if (root.innerWidth <= 700 || rect.top < 20 || rect.bottom > root.innerHeight - 60) {
        root.scrollBy({ top: rect.top - 70, behavior: 'instant' });
      }
    }
    position(); panel.querySelector('.oa-guide-next').focus({ preventScroll: true });
  }
  function close() {
    if (!active) return;
    active = false; gate.dismiss(); panel.remove(); marker.remove(); panel = marker = null;
    root.removeEventListener('resize', schedulePosition); root.removeEventListener('scroll', schedulePosition, true);
    doc.removeEventListener('keydown', onKey);
    if (pendingFrame) root.cancelAnimationFrame(pendingFrame);
    pendingFrame = null;
    const restore = previousFocus?.isConnected && visible(previousFocus) && previousFocus !== doc.body ? previousFocus : help;
    restore?.focus();
  }
  function onKey(event) { if (event.key === 'Escape' && active) { event.preventDefault(); close(); } }
  function start() {
    mount();
    if (active || !usable()) return false;
    gate.started(); active = true; index = 0; previousFocus = doc.activeElement;
    marker = doc.createElement('div'); marker.className = 'oa-guide-target'; marker.setAttribute('aria-hidden', 'true');
    panel = doc.createElement('section'); panel.className = 'oa-guide'; panel.setAttribute('role', 'dialog'); panel.setAttribute('aria-modal', 'false');
    panel.setAttribute('aria-labelledby', 'oa-guide-title'); panel.setAttribute('aria-describedby', 'oa-guide-description');
    const top = doc.createElement('div'); top.className = 'oa-guide-top';
    const progress = doc.createElement('span'); progress.className = 'oa-guide-progress'; progress.setAttribute('aria-live', 'polite');
    const dismiss = button('×', close, 'oa-guide-close'); dismiss.setAttribute('aria-label', '关闭使用导引，7 天后再次提示');
    top.append(progress, dismiss);
    const title = doc.createElement('h2'); title.id = 'oa-guide-title';
    const text = doc.createElement('p'); text.id = 'oa-guide-description';
    const footnote = doc.createElement('p'); footnote.className = 'oa-guide-note'; footnote.textContent = '完成或跳过后 7 天再提示 · 随时从页首重看';
    const actions = doc.createElement('div'); actions.className = 'oa-guide-actions';
    actions.append(button('跳过', close, 'oa-guide-skip'), button('上一步', () => { if (index > 0) { index--; render(); } }, 'oa-guide-back'), button('下一步 →', () => { if (index === steps.length - 1) close(); else { index++; render(); } }, 'oa-guide-next'));
    const content = doc.createElement('div'); content.setAttribute('aria-live', 'polite'); content.setAttribute('aria-atomic', 'true'); content.append(title, text);
    panel.append(top, content, footnote, actions); doc.body.append(marker, panel);
    root.addEventListener('resize', schedulePosition); root.addEventListener('scroll', schedulePosition, true); doc.addEventListener('keydown', onKey);
    render(); return true;
  }
  function maybeStart() { mount(); return usable() && gate.eligible() ? start() : false; }
  function onReady() { ready = true; mount(); if (help) help.disabled = false; maybeStart(); }
  root.OpenAlexOnboarding = Object.freeze({ mount, maybeStart, start });
  doc.addEventListener('openalex:ready', onReady);
  if (doc.readyState === 'loading') doc.addEventListener('DOMContentLoaded', mount, { once: true }); else mount();
})(globalThis);
