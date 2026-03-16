<script>
  import { onMount, onDestroy } from 'svelte';
  import { prefs, setPref } from '$lib/stores/theme.js';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  const CELL = 80;
  const GAP  = 10;
  const MARGIN = 16;

  const WIDGET_TYPES = {
    clock:   { name: 'Reloj',    icon: '🕐', cols: 3, rows: 2 },
    sysmon:  { name: 'Sistema',  icon: '📊', cols: 3, rows: 3 },
    storage: { name: 'Storage',  icon: '💾', cols: 3, rows: 2 },
    network: { name: 'Red',      icon: '🌐', cols: 3, rows: 2 },
  };

  const SIZE_PRESETS = {
    clock:   [{ label: 'Pequeño', cols:2, rows:2 }, { label: 'Normal', cols:3, rows:2 }, { label: 'Grande', cols:4, rows:2 }],
    sysmon:  [{ label: 'Normal',  cols:3, rows:3 }, { label: 'Grande', cols:4, rows:3 }],
    storage: [{ label: 'Normal',  cols:3, rows:2 }, { label: 'Grande', cols:4, rows:3 }],
    network: [{ label: 'Normal',  cols:3, rows:2 }, { label: 'Grande', cols:4, rows:3 }],
  };

  const DEFAULT_LAYOUT = [
    { id: 'w1', type: 'clock',   col: 0,  row: 0, cols: 3, rows: 2 },
    { id: 'w2', type: 'sysmon',  col: 0,  row: 2, cols: 3, rows: 3 },
    { id: 'w3', type: 'storage', col: -3, row: 0, cols: 3, rows: 2 },
    { id: 'w4', type: 'network', col: -3, row: 2, cols: 3, rows: 2 },
  ];

  let widgets = [];
  let gridCols = 0;
  let gridRows = 0;
  let layoutLoaded = false;

  // ── Context menu ──
  let activeMenu = null; // { widgetId, x, y, sub: null | 'size' | 'add' }

  function openMenu(e, widgetId) {
    e.stopPropagation();
    if (activeMenu?.widgetId === widgetId) { activeMenu = null; return; }
    // Position menu relative to the button, not mouse
    const btn = e.currentTarget;
    const rect = btn.getBoundingClientRect();
    // Place menu to the left of button, above it
    const x = Math.min(rect.right, window.innerWidth - 210);
    const y = rect.top;
    activeMenu = { widgetId, x, y, sub: null };
  }

  function openAddMenu(e) {
    e.stopPropagation();
    activeMenu = activeMenu?.widgetId === '_add' ? null : { widgetId: '_add', x: e.clientX, y: e.clientY, sub: null };
  }

  function closeMenu() { activeMenu = null; }

  // ── Grid ──
  function getZoom() { return parseFloat(document.documentElement.style.zoom) || 1; }

  function getTbInfo() {
    const tbH = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--taskbar-height')) || 48;
    const tbPos = document.documentElement.getAttribute('data-taskbar-pos') || 'bottom';
    return { tbH, tbPos };
  }

  function computeGrid() {
    const z = getZoom();
    const vw = window.innerWidth / z;
    const vh = window.innerHeight / z;
    const { tbH, tbPos } = getTbInfo();
    let areaW = vw - MARGIN * 2;
    let areaH = vh - tbH - MARGIN * 2;
    if (tbPos === 'left') { areaW = vw - tbH - MARGIN * 2; areaH = vh - MARGIN * 2; }
    gridCols = Math.floor((areaW + GAP) / (CELL + GAP));
    gridRows = Math.floor((areaH + GAP) / (CELL + GAP));
  }

  function resolveLayout(layout) {
    computeGrid();
    return layout.map(w => {
      let col = w.col < 0 ? gridCols + w.col : w.col;
      col = Math.max(0, Math.min(gridCols - w.cols, col));
      const row = Math.max(0, Math.min(gridRows - w.rows, w.row));
      return { ...w, col, row };
    });
  }

  function loadLayout() {
    const saved = $prefs.widgetLayout;
    widgets = resolveLayout(saved?.length ? saved : DEFAULT_LAYOUT);
    layoutLoaded = true;
  }

  function saveLayout() {
    if (!layoutLoaded) return;
    setPref('widgetLayout', widgets.map(({ id, type, col, row, cols, rows }) => ({ id, type, col, row, cols, rows })));
  }

  let prevLayoutJson = '';
  $: {
    const lj = JSON.stringify($prefs.widgetLayout);
    if (lj !== prevLayoutJson && !dragging) {
      prevLayoutJson = lj;
      if (typeof window !== 'undefined') loadLayout();
    }
  }

  onMount(() => { computeGrid(); loadLayout(); startPolling(); window.addEventListener('resize', onResize); });
  onDestroy(() => { stopPolling(); window.removeEventListener('resize', onResize); });

  function onResize() {
    computeGrid();
    widgets = widgets.map(w => ({
      ...w,
      col: Math.max(0, Math.min(gridCols - w.cols, w.col)),
      row: Math.max(0, Math.min(gridRows - w.rows, w.row)),
    }));
  }

  function cellX(col) {
    const { tbH, tbPos } = getTbInfo();
    return (tbPos === 'left' ? tbH : 0) + MARGIN + col * (CELL + GAP);
  }
  function cellY(row) {
    const { tbH, tbPos } = getTbInfo();
    return (tbPos === 'top' ? tbH : 0) + MARGIN + row * (CELL + GAP);
  }
  function cellW(cols) { return cols * CELL + (cols - 1) * GAP; }
  function cellH(rows) { return rows * CELL + (rows - 1) * GAP; }

  // ── Drag ──
  let dragging = null;
  let dragPreviewCol = 0;
  let dragPreviewRow = 0;
  let dragWidget = null;

  function onDragStart(e, widget) {
    if (e.target.closest('.wm-btn')) return;
    e.preventDefault();
    closeMenu();
    const z = getZoom();
    dragging = { id: widget.id, startMx: e.clientX / z, startMy: e.clientY / z, origCol: widget.col, origRow: widget.row };
    dragWidget = widget;
    dragPreviewCol = widget.col;
    dragPreviewRow = widget.row;
    window.addEventListener('mousemove', onDragMove);
    window.addEventListener('mouseup', onDragEnd);
  }

  function onDragMove(e) {
    if (!dragging || !dragWidget) return;
    const z = getZoom();
    const dx = (e.clientX / z) - dragging.startMx;
    const dy = (e.clientY / z) - dragging.startMy;
    dragPreviewCol = Math.max(0, Math.min(gridCols - dragWidget.cols, dragging.origCol + Math.round(dx / (CELL + GAP))));
    dragPreviewRow = Math.max(0, Math.min(gridRows - dragWidget.rows, dragging.origRow + Math.round(dy / (CELL + GAP))));
  }

  function onDragEnd() {
    if (dragging && dragWidget) {
      const idx = widgets.findIndex(w => w.id === dragging.id);
      if (idx >= 0) {
        // Only move if no collision at new position
        if (!hasCollision(dragPreviewCol, dragPreviewRow, dragWidget.cols, dragWidget.rows, dragging.id)) {
          widgets[idx].col = dragPreviewCol;
          widgets[idx].row = dragPreviewRow;
        }
        // else: revert to original position (no change)
        widgets = widgets;
        saveLayout();
      }
    }
    dragging = null; dragWidget = null;
    window.removeEventListener('mousemove', onDragMove);
    window.removeEventListener('mouseup', onDragEnd);
  }

  // ── Widget actions ──
  function removeWidget(id) { widgets = widgets.filter(w => w.id !== id); saveLayout(); closeMenu(); }

  function hasCollision(col, row, cols, rows, excludeId = null) {
    return widgets.some(w => {
      if (w.id === excludeId) return false;
      return col < w.col + w.cols && col + cols > w.col &&
             row < w.row + w.rows && row + rows > w.row;
    });
  }

  function addWidget(type) {
    const meta = WIDGET_TYPES[type];
    const id = 'w' + Date.now();
    let placed = false;
    // Try every grid position
    outer: for (let r = 0; r <= gridRows - meta.rows; r++) {
      for (let c = 0; c <= gridCols - meta.cols; c++) {
        if (!hasCollision(c, r, meta.cols, meta.rows)) {
          widgets = [...widgets, { id, type, col: c, row: r, cols: meta.cols, rows: meta.rows }];
          placed = true;
          break outer;
        }
      }
    }
    // Fallback: place at 0,0 even if overlap
    if (!placed) widgets = [...widgets, { id, type, col: 0, row: 0, cols: meta.cols, rows: meta.rows }];
    saveLayout(); closeMenu();
  }

  function resizeWidget(id, cols, rows) {
    const idx = widgets.findIndex(w => w.id === id);
    if (idx < 0) { closeMenu(); return; }
    const w = widgets[idx];
    // Check if resize causes collision
    if (!hasCollision(w.col, w.row, cols, rows, id)) {
      widgets[idx] = { ...w, cols, rows,
        col: Math.min(w.col, gridCols - cols),
        row: Math.min(w.row, gridRows - rows),
      };
      widgets = widgets;
      saveLayout();
    }
    closeMenu();
  }

  function resetLayout() { widgets = resolveLayout(DEFAULT_LAYOUT); saveLayout(); closeMenu(); }

  // ── Data polling ──
  let pollTimer;
  let sysData = {};
  let storageData = {};
  let netData = {};

  async function fetchData() {
    try {
      const [sys, stor, net] = await Promise.all([
        fetch('/api/system',         { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/storage/status', { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/network',        { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
      ]);
      sysData = sys || {}; storageData = stor || {}; netData = net || {};
    } catch {}
  }
  function startPolling() { fetchData(); pollTimer = setInterval(fetchData, 5000); }
  function stopPolling()  { if (pollTimer) clearInterval(pollTimer); }

  // ── Clock ──
  let clockTime = '';
  let clockDate = '';
  function updateClock() {
    const now = new Date();
    clockTime = now.toLocaleTimeString('es-ES', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    clockDate = now.toLocaleDateString('es-ES', { weekday: 'long', day: 'numeric', month: 'long' });
  }
  updateClock();
  const clockInterval = setInterval(updateClock, 1000);
  onDestroy(() => clearInterval(clockInterval));

  function fmtBytes(b) {
    if (!b || b <= 0) return '—';
    if (b >= 1e12) return (b / 1e12).toFixed(1) + ' TB';
    if (b >= 1e9)  return (b / 1e9).toFixed(1)  + ' GB';
    return (b / 1e6).toFixed(0) + ' MB';
  }

  $: cpuPct   = sysData.cpu?.percent    ?? sysData.cpuPercent ?? 0;
  $: memPct   = sysData.memory?.percent ?? sysData.memPercent ?? 0;
  $: memUsed  = sysData.memory?.used    ?? 0;
  $: memTotal = sysData.memory?.total   ?? 0;
  $: cpuTemp  = sysData.temps?.cpu      ?? sysData.cpuTemp    ?? null;
  $: pools    = storageData.pools       || [];
  $: netIfaces   = Array.isArray(netData) ? netData : (netData.interfaces || []);
  $: primaryIface = netIfaces.find(i => i.ip && i.ip !== '127.0.0.1') || netIfaces[0] || {};
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="widget-layer" on:click={closeMenu}>

  {#if $prefs.showWidgets}
    {#each widgets as widget (widget.id)}
      {@const isDrag = dragging?.id === widget.id}
      {@const dc = isDrag ? dragPreviewCol : widget.col}
      {@const dr = isDrag ? dragPreviewRow : widget.row}
      {@const menuOpen = activeMenu?.widgetId === widget.id}

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="wc"
        class:is-dragging={isDrag}
        class:menu-open={menuOpen}
        style="left:{cellX(dc)}px; top:{cellY(dr)}px; width:{cellW(widget.cols)}px; height:{cellH(widget.rows)}px;"
        on:mousedown={(e) => onDragStart(e, widget)}
      >
        <!-- 3-dot menu button -->
        <button class="wm-btn" on:click={(e) => openMenu(e, widget.id)} title="Opciones">
          <span class="wm-dot"></span>
          <span class="wm-dot"></span>
          <span class="wm-dot"></span>
        </button>

        <!-- Widget content -->
        <div class="wc-body">
          {#if widget.type === 'clock'}
            <div class="wg-clock">
              <div class="wg-clock-t">{clockTime}</div>
              <div class="wg-clock-d">{clockDate}</div>
            </div>

          {:else if widget.type === 'sysmon'}
            <div class="wg-header">Sistema</div>
            <div class="wg-bars">
              <div class="wg-bar-row">
                <span class="wg-label">CPU</span>
                <div class="wg-track"><div class="wg-fill cpu" style="width:{Math.min(100,cpuPct)}%"></div></div>
                <span class="wg-val">{cpuPct.toFixed(0)}%</span>
              </div>
              <div class="wg-bar-row">
                <span class="wg-label">RAM</span>
                <div class="wg-track"><div class="wg-fill ram" style="width:{Math.min(100,memPct)}%"></div></div>
                <span class="wg-val">{memPct.toFixed(0)}%</span>
              </div>
            </div>
            <div class="wg-footer">
              {#if cpuTemp}<span>🌡 {cpuTemp}°C</span>{/if}
              <span>{fmtBytes(memUsed)} / {fmtBytes(memTotal)}</span>
            </div>

          {:else if widget.type === 'storage'}
            <div class="wg-header">Storage</div>
            {#if pools.length > 0}
              {#each pools.slice(0,2) as pool}
                <div class="wg-bar-row">
                  <span class="wg-label">{pool.name}</span>
                  <div class="wg-track"><div class="wg-fill sto" style="width:{pool.usagePercent||0}%"></div></div>
                  <span class="wg-val">{pool.usagePercent||0}%</span>
                </div>
                <div class="wg-sub">{pool.usedFormatted||'—'} / {pool.totalFormatted||'—'}</div>
              {/each}
            {:else}
              <div class="wg-empty">Sin pools configurados</div>
            {/if}

          {:else if widget.type === 'network'}
            <div class="wg-header">Red</div>
            {#if primaryIface.name}
              <div class="wg-kv"><span>Interfaz</span><span>{primaryIface.name}</span></div>
              <div class="wg-kv"><span>IP local</span><span>{primaryIface.ip||'—'}</span></div>
              <div class="wg-kv"><span>Velocidad</span><span>{primaryIface.speed||'—'}</span></div>
            {:else}
              <div class="wg-empty">Sin conexión</div>
            {/if}
          {/if}
        </div>

      </div>

      <!-- Drag ghost — sibling of widget, not child -->
      {#if isDrag}
        <div class="snap-ghost"
          style="left:{cellX(dragPreviewCol)}px; top:{cellY(dragPreviewRow)}px; width:{cellW(widget.cols)}px; height:{cellH(widget.rows)}px;">
        </div>
      {/if}

      <!-- Context menu for this widget -->
      {#if menuOpen}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-menu" style="left:{activeMenu.x}px; top:{activeMenu.y}px; transform:translate(-100%, -100%);"
          on:click|stopPropagation>

          {#if activeMenu.sub === 'size'}
            <!-- Size submenu -->
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-back" on:click={() => activeMenu = { ...activeMenu, sub: null }}>
              ‹ Volver
            </div>
            <div class="ctx-divider"></div>
            {#each SIZE_PRESETS[widget.type] || [] as preset}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="ctx-item"
                class:active={widget.cols === preset.cols && widget.rows === preset.rows}
                on:click={() => resizeWidget(widget.id, preset.cols, preset.rows)}>
                <span class="ctx-ico">◻</span>
                {preset.label}
                <span class="ctx-size-hint">{preset.cols}×{preset.rows}</span>
              </div>
            {/each}

          {:else if activeMenu.sub === 'add'}
            <!-- Add widget submenu -->
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-back" on:click={() => activeMenu = { ...activeMenu, sub: null }}>
              ‹ Volver
            </div>
            <div class="ctx-divider"></div>
            {#each Object.entries(WIDGET_TYPES) as [type, meta]}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="ctx-item" on:click={() => addWidget(type)}>
                <span class="ctx-ico">{meta.icon}</span>
                {meta.name}
              </div>
            {/each}

          {:else}
            <!-- Main menu -->
            <div class="ctx-header">
              <span>{WIDGET_TYPES[widget.type]?.icon}</span>
              <span>{WIDGET_TYPES[widget.type]?.name}</span>
            </div>
            <div class="ctx-divider"></div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item" on:click={() => activeMenu = { ...activeMenu, sub: 'add' }}>
              <span class="ctx-ico">＋</span> Añadir widget ›
            </div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item" on:click={() => activeMenu = { ...activeMenu, sub: 'size' }}>
              <span class="ctx-ico">◻</span> Cambiar tamaño ›
            </div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item" on:click={resetLayout}>
              <span class="ctx-ico">⊞</span> Restablecer grid
            </div>
            <div class="ctx-divider"></div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item danger" on:click={() => removeWidget(widget.id)}>
              <span class="ctx-ico">✕</span> Eliminar
            </div>
          {/if}
        </div>
      {/if}
    {/each}

    <!-- Floating add button -->
    <div class="wa-wrap">
      <button class="wa-btn" on:click={openAddMenu} title="Añadir widget">
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M7 1v12M1 7h12"/>
        </svg>
      </button>

      {#if activeMenu?.widgetId === '_add'}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="wa-menu" on:click|stopPropagation>
          <div class="ctx-header-label">Añadir widget</div>
          {#each Object.entries(WIDGET_TYPES) as [type, meta]}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="wa-item" on:click={() => addWidget(type)}>
              <span class="wa-ico">{meta.icon}</span>
              <div class="wa-item-info">
                <span class="wa-item-name">{meta.name}</span>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .widget-layer {
    position: fixed; inset: 0; z-index: 1;
    pointer-events: none;
  }

  /* ── WIDGET CARD ── */
  .wc {
    position: absolute; pointer-events: auto;
    border-radius: 14px; overflow: visible;
    background: var(--widget-bg, rgba(17,16,40,0.52));
    backdrop-filter: blur(20px) saturate(1.4);
    -webkit-backdrop-filter: blur(20px) saturate(1.4);
    border: 1px solid var(--border);
    box-shadow: 0 4px 24px rgba(0,0,0,0.18);
    cursor: grab; user-select: none;
    transition: left .22s cubic-bezier(.25,1,.5,1), top .22s cubic-bezier(.25,1,.5,1), box-shadow .2s, border-color .2s;
  }
  .wc:hover { border-color: var(--border-hi); box-shadow: 0 8px 32px rgba(0,0,0,0.25); }
  .wc.is-dragging { cursor: grabbing; opacity: .88; transition: none; z-index: 10; box-shadow: 0 16px 48px rgba(0,0,0,0.40); }
  .wc.menu-open { border-color: var(--border-hi); }

  /* Theme variants */
  :global([data-theme="dark"]) .wc { background: rgba(22,22,22,0.58); }
  :global([data-theme="light"]) .wc {
    background: rgba(240,240,244,0.72);
    border-color: var(--border);
    box-shadow: 0 4px 20px rgba(0,0,0,0.08);
  }

  /* ── 3-DOT BUTTON ── */
  .wm-btn {
    position: absolute; top: 8px; right: 8px;
    width: 22px; height: 22px; border-radius: 6px;
    border: none; background: transparent;
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2.5px;
    cursor: pointer; opacity: 0; transition: opacity .15s, background .15s;
    z-index: 5; padding: 0;
  }
  .wc:hover .wm-btn, .wc.menu-open .wm-btn { opacity: 1; }
  .wm-btn:hover { background: var(--ibtn-bg); }
  .wm-dot {
    width: 3px; height: 3px; border-radius: 50%;
    background: var(--text-2); flex-shrink: 0;
  }

  /* ── WIDGET BODY ── */
  .wc-body {
    width: 100%; height: 100%;
    padding: 12px 14px;
    display: flex; flex-direction: column;
    overflow: hidden;
  }

  .wg-header {
    font-size: 9px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: .08em;
    margin-bottom: 10px; flex-shrink: 0;
  }

  /* Clock */
  .wg-clock {
    display: flex; flex-direction: column;
    align-items: center; justify-content: center;
    height: 100%; gap: 4px;
  }
  .wg-clock-t {
    font-size: 30px; font-weight: 300; letter-spacing: -.02em;
    color: var(--text-1); font-family: 'DM Mono', monospace; line-height: 1;
  }
  .wg-clock-d {
    font-size: 10px; color: var(--text-3); text-transform: capitalize;
    font-family: 'DM Sans', sans-serif;
  }

  /* Bars */
  .wg-bars { display: flex; flex-direction: column; gap: 9px; flex: 1; }
  .wg-bar-row { display: flex; align-items: center; gap: 8px; }
  .wg-label { font-size: 10px; font-weight: 600; color: var(--text-2); width: 30px; flex-shrink: 0; }
  .wg-track {
    flex: 1; height: 5px; border-radius: 3px;
    background: var(--ibtn-bg); overflow: hidden;
  }
  .wg-fill { height: 100%; border-radius: 3px; transition: width .6s ease; }
  .wg-fill.cpu { background: linear-gradient(90deg, var(--accent), var(--accent2, #c054f0)); }
  .wg-fill.ram { background: linear-gradient(90deg, #60a5fa, #818cf8); }
  .wg-fill.sto { background: linear-gradient(90deg, #4ade80, #22d3ee); }
  .wg-val { font-size: 10px; color: var(--text-2); font-family: 'DM Mono', monospace; width: 32px; text-align: right; flex-shrink: 0; }

  .wg-footer {
    display: flex; gap: 12px; margin-top: auto;
    padding-top: 8px; border-top: 1px solid var(--border);
    font-size: 10px; color: var(--text-3); font-family: 'DM Mono', monospace;
    flex-shrink: 0;
  }

  .wg-sub { font-size: 9px; color: var(--text-3); font-family: 'DM Mono', monospace; margin: 1px 0 4px 38px; }
  .wg-empty { font-size: 11px; color: var(--text-3); flex: 1; display: flex; align-items: center; }

  /* KV */
  .wg-kv {
    display: flex; justify-content: space-between; align-items: center;
    padding: 5px 0; border-bottom: 1px solid var(--border);
  }
  .wg-kv:last-child { border-bottom: none; }
  .wg-kv span:first-child { font-size: 10px; color: var(--text-3); }
  .wg-kv span:last-child  { font-size: 11px; color: var(--text-1); font-family: 'DM Mono', monospace; }

  /* Drag ghost */
  .snap-ghost {
    position: fixed; border-radius: 14px;
    border: 2px dashed var(--accent);
    background: rgba(124,111,255,0.06);
    pointer-events: none; z-index: 0;
    transition: left .12s ease, top .12s ease;
  }

  /* ── CONTEXT MENU ── */
  .ctx-menu {
    position: fixed; z-index: 9999;
    min-width: 200px;
    background: var(--bg-inner);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 16px 40px rgba(0,0,0,0.4), 0 2px 8px rgba(0,0,0,0.2);
    overflow: hidden;
    animation: ctxIn .12s cubic-bezier(0.16,1,0.3,1) both;
    pointer-events: auto;
    /* Position set via JS — no transform needed */
  }
  @keyframes ctxIn {
    from { opacity: 0; transform: scale(.97); }
    to   { opacity: 1; transform: scale(1); }
  }
  .ctx-header {
    display: flex; align-items: center; gap: 8px;
    padding: 10px 12px 8px;
    font-size: 12px; font-weight: 600; color: var(--text-1);
  }
  .ctx-header-label {
    padding: 10px 12px 6px;
    font-size: 9px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: .08em;
  }
  .ctx-divider { height: 1px; background: var(--border); }
  .ctx-item {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 12px; font-size: 12px; color: var(--text-2);
    cursor: pointer; transition: all .1s;
  }
  .ctx-item:hover { background: var(--active-bg); color: var(--text-1); }
  .ctx-item.active { color: var(--accent); }
  .ctx-item.danger { color: var(--red); }
  .ctx-item.danger:hover { background: rgba(248,113,113,0.10); }
  .ctx-ico { font-size: 11px; width: 14px; text-align: center; color: var(--text-3); flex-shrink: 0; }
  .ctx-item.danger .ctx-ico { color: var(--red); }
  .ctx-size-hint { margin-left: auto; font-size: 9px; color: var(--text-3); font-family: 'DM Mono', monospace; }
  .ctx-back {
    display: flex; align-items: center; gap: 6px;
    padding: 8px 12px; font-size: 11px; color: var(--accent);
    cursor: pointer; transition: opacity .15s;
  }
  .ctx-back:hover { opacity: .7; }

  /* ── ADD BUTTON ── */
  .wa-wrap {
    position: fixed; bottom: 68px; right: 16px;
    z-index: 2; pointer-events: auto;
  }
  .wa-btn {
    width: 34px; height: 34px; border-radius: 50%;
    border: 1px solid var(--border);
    background: var(--bg-inner);
    backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
    color: var(--text-2); cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    transition: all .15s; box-shadow: 0 4px 12px rgba(0,0,0,0.2);
  }
  .wa-btn:hover { background: var(--active-bg); color: var(--text-1); border-color: var(--border-hi); }

  .wa-menu {
    position: absolute; bottom: 42px; right: 0;
    min-width: 180px;
    background: var(--bg-inner); border: 1px solid var(--border);
    border-radius: 10px; overflow: hidden;
    box-shadow: 0 12px 36px rgba(0,0,0,0.35);
    animation: ctxIn .12s ease both;
  }
  .wa-item {
    display: flex; align-items: center; gap: 10px;
    padding: 9px 14px; cursor: pointer;
    border: none; background: none; width: 100%;
    font-family: inherit; transition: all .1s; text-align: left;
    color: var(--text-2);
  }
  .wa-item:hover { background: var(--active-bg); color: var(--text-1); }
  .wa-ico { font-size: 16px; flex-shrink: 0; }
  .wa-item-name { font-size: 12px; font-weight: 500; color: var(--text-1); }
</style>
