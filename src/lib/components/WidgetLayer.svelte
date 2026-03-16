<script>
  import { onMount, onDestroy } from 'svelte';
  import { prefs, setPref } from '$lib/stores/theme.js';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  // ── Widget registry ──
  const WIDGET_TYPES = {
    clock:   { name: 'Reloj',     icon: '🕐', defaultW: 220, defaultH: 120, minW: 160, minH: 90 },
    sysmon:  { name: 'Sistema',   icon: '📊', defaultW: 260, defaultH: 180, minW: 200, minH: 140 },
    storage: { name: 'Storage',   icon: '💾', defaultW: 240, defaultH: 150, minW: 180, minH: 110 },
    network: { name: 'Red',       icon: '🌐', defaultW: 240, defaultH: 140, minW: 180, minH: 100 },
  };

  // ── Layout state ──
  // Each widget: { id, type, xPct, yPct, wPx, hPx }
  let widgets = [];
  let areaEl;

  const DEFAULT_LAYOUT = [
    { id: 'w1', type: 'clock',   xPct: 2,  yPct: 3,  wPx: 220, hPx: 120 },
    { id: 'w2', type: 'sysmon',  xPct: 2,  yPct: 22, wPx: 260, hPx: 200 },
    { id: 'w3', type: 'storage', xPct: 75, yPct: 3,  wPx: 240, hPx: 150 },
    { id: 'w4', type: 'network', xPct: 75, yPct: 28, wPx: 240, hPx: 140 },
  ];

  function loadLayout() {
    const saved = $prefs.widgetLayout;
    if (saved && Array.isArray(saved) && saved.length > 0) {
      widgets = saved;
    } else {
      widgets = [...DEFAULT_LAYOUT];
    }
  }

  function saveLayout() {
    setPref('widgetLayout', widgets.map(w => ({ ...w })));
  }

  onMount(() => { loadLayout(); startPolling(); });
  onDestroy(() => stopPolling());

  // ── Usable area (accounts for taskbar) ──
  function getUsableArea() {
    if (!areaEl) return { x: 0, y: 0, w: window.innerWidth, h: window.innerHeight };
    const r = areaEl.getBoundingClientRect();
    const zoom = parseFloat(document.documentElement.style.zoom) || 1;
    return { x: r.left / zoom, y: r.top / zoom, w: r.width / zoom, h: r.height / zoom };
  }

  // ── Drag ──
  let dragging = null; // { id, startMx, startMy, startXPct, startYPct }

  function onDragStart(e, widget) {
    if (e.target.closest('.wr-handle')) return; // resize handle
    e.preventDefault();
    const zoom = parseFloat(document.documentElement.style.zoom) || 1;
    dragging = {
      id: widget.id,
      startMx: e.clientX / zoom,
      startMy: e.clientY / zoom,
      startXPct: widget.xPct,
      startYPct: widget.yPct,
    };
    window.addEventListener('mousemove', onDragMove);
    window.addEventListener('mouseup', onDragEnd);
  }

  function onDragMove(e) {
    if (!dragging) return;
    const zoom = parseFloat(document.documentElement.style.zoom) || 1;
    const area = getUsableArea();
    const dx = (e.clientX / zoom) - dragging.startMx;
    const dy = (e.clientY / zoom) - dragging.startMy;
    const dxPct = (dx / area.w) * 100;
    const dyPct = (dy / area.h) * 100;
    const idx = widgets.findIndex(w => w.id === dragging.id);
    if (idx >= 0) {
      widgets[idx].xPct = Math.max(0, Math.min(95, dragging.startXPct + dxPct));
      widgets[idx].yPct = Math.max(0, Math.min(95, dragging.startYPct + dyPct));
      widgets = widgets;
    }
  }

  function onDragEnd() {
    dragging = null;
    window.removeEventListener('mousemove', onDragMove);
    window.removeEventListener('mouseup', onDragEnd);
    saveLayout();
  }

  // ── Resize ──
  let resizing = null; // { id, startMx, startMy, startW, startH }

  function onResizeStart(e, widget) {
    e.preventDefault();
    e.stopPropagation();
    const zoom = parseFloat(document.documentElement.style.zoom) || 1;
    resizing = {
      id: widget.id,
      startMx: e.clientX / zoom,
      startMy: e.clientY / zoom,
      startW: widget.wPx,
      startH: widget.hPx,
    };
    window.addEventListener('mousemove', onResizeMove);
    window.addEventListener('mouseup', onResizeEnd);
  }

  function onResizeMove(e) {
    if (!resizing) return;
    const zoom = parseFloat(document.documentElement.style.zoom) || 1;
    const dx = (e.clientX / zoom) - resizing.startMx;
    const dy = (e.clientY / zoom) - resizing.startMy;
    const idx = widgets.findIndex(w => w.id === resizing.id);
    if (idx >= 0) {
      const meta = WIDGET_TYPES[widgets[idx].type] || {};
      widgets[idx].wPx = Math.max(meta.minW || 140, resizing.startW + dx);
      widgets[idx].hPx = Math.max(meta.minH || 90, resizing.startH + dy);
      widgets = widgets;
    }
  }

  function onResizeEnd() {
    resizing = null;
    window.removeEventListener('mousemove', onResizeMove);
    window.removeEventListener('mouseup', onResizeEnd);
    saveLayout();
  }

  // ── Remove widget ──
  function removeWidget(id) {
    widgets = widgets.filter(w => w.id !== id);
    saveLayout();
  }

  // ── Add widget ──
  let showAddMenu = false;

  function addWidget(type) {
    const meta = WIDGET_TYPES[type];
    const id = 'w' + Date.now();
    widgets = [...widgets, {
      id, type,
      xPct: 30 + Math.random() * 20,
      yPct: 20 + Math.random() * 20,
      wPx: meta.defaultW,
      hPx: meta.defaultH,
    }];
    showAddMenu = false;
    saveLayout();
  }

  // ── Data polling ──
  let pollTimer;
  let sysData = {};
  let storageData = {};
  let netData = {};

  async function fetchData() {
    try {
      const [sys, stor, net] = await Promise.all([
        fetch('/api/system', { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/storage/status', { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/network', { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
      ]);
      sysData = sys || {};
      storageData = stor || {};
      netData = net || {};
    } catch {}
  }

  function startPolling() {
    fetchData();
    pollTimer = setInterval(fetchData, 5000);
  }
  function stopPolling() { if (pollTimer) clearInterval(pollTimer); }

  // ── Clock ──
  let clockTime = '';
  let clockDate = '';
  function updateClock() {
    const now = new Date();
    clockTime = now.toLocaleTimeString('es-ES', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    clockDate = now.toLocaleDateString('es-ES', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  }
  updateClock();
  const clockInterval = setInterval(updateClock, 1000);
  onDestroy(() => clearInterval(clockInterval));

  // ── Helpers ──
  function fmtBytes(b) {
    if (!b || b <= 0) return '—';
    if (b >= 1e12) return (b / 1e12).toFixed(1) + ' TB';
    if (b >= 1e9) return (b / 1e9).toFixed(1) + ' GB';
    if (b >= 1e6) return (b / 1e6).toFixed(0) + ' MB';
    return (b / 1e3).toFixed(0) + ' KB';
  }

  $: cpuPct = sysData.cpu?.percent ?? sysData.cpuPercent ?? 0;
  $: memPct = sysData.memory?.percent ?? sysData.memPercent ?? 0;
  $: memUsed = sysData.memory?.used ?? 0;
  $: memTotal = sysData.memory?.total ?? 0;
  $: cpuTemp = sysData.temps?.cpu ?? sysData.cpuTemp ?? null;
  $: pools = storageData.pools || [];
  $: hasPool = pools.length > 0;
  $: netIfaces = Array.isArray(netData) ? netData : (netData.interfaces || []);
  $: primaryIface = netIfaces.find(i => i.ip && i.ip !== '127.0.0.1') || netIfaces[0] || {};
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="widget-layer" bind:this={areaEl} on:click={() => showAddMenu = false}>
  {#if $prefs.showWidgets}
    {#each widgets as widget (widget.id)}
      {@const area = getUsableArea()}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="widget-wrap"
        class:is-dragging={dragging?.id === widget.id}
        class:is-resizing={resizing?.id === widget.id}
        style="left:{widget.xPct}%; top:{widget.yPct}%; width:{widget.wPx}px; height:{widget.hPx}px;"
        on:mousedown={(e) => onDragStart(e, widget)}
      >
        <!-- Close button -->
        <button class="wr-close" on:click|stopPropagation={() => removeWidget(widget.id)} title="Quitar widget">×</button>

        <!-- Content -->
        <div class="wr-content">
          {#if widget.type === 'clock'}
            <div class="wg-clock">
              <div class="wg-clock-time">{clockTime}</div>
              <div class="wg-clock-date">{clockDate}</div>
            </div>

          {:else if widget.type === 'sysmon'}
            <div class="wg-sysmon">
              <div class="wg-sysmon-title">Sistema</div>
              <div class="wg-bar-group">
                <div class="wg-bar-row">
                  <span class="wg-bar-label">CPU</span>
                  <div class="wg-bar-track"><div class="wg-bar-fill cpu" style="width:{Math.min(100, cpuPct)}%"></div></div>
                  <span class="wg-bar-val">{cpuPct.toFixed(0)}%</span>
                </div>
                <div class="wg-bar-row">
                  <span class="wg-bar-label">RAM</span>
                  <div class="wg-bar-track"><div class="wg-bar-fill ram" style="width:{Math.min(100, memPct)}%"></div></div>
                  <span class="wg-bar-val">{memPct.toFixed(0)}%</span>
                </div>
              </div>
              <div class="wg-sysmon-footer">
                {#if cpuTemp}<span>🌡 {cpuTemp}°C</span>{/if}
                <span>{fmtBytes(memUsed)} / {fmtBytes(memTotal)}</span>
              </div>
            </div>

          {:else if widget.type === 'storage'}
            <div class="wg-storage">
              <div class="wg-storage-title">Storage</div>
              {#if hasPool}
                {#each pools.slice(0, 2) as pool}
                  <div class="wg-pool-row">
                    <span class="wg-pool-name">{pool.name}</span>
                    <div class="wg-bar-track"><div class="wg-bar-fill storage" style="width:{pool.usagePercent || 0}%"></div></div>
                    <span class="wg-bar-val">{pool.usagePercent || 0}%</span>
                  </div>
                  <div class="wg-pool-detail">{pool.usedFormatted || '—'} / {pool.totalFormatted || '—'}</div>
                {/each}
              {:else}
                <div class="wg-empty">Sin pools</div>
              {/if}
            </div>

          {:else if widget.type === 'network'}
            <div class="wg-network">
              <div class="wg-network-title">Red</div>
              {#if primaryIface.name}
                <div class="wg-net-row"><span class="wg-net-label">Interfaz</span><span class="wg-net-val">{primaryIface.name}</span></div>
                <div class="wg-net-row"><span class="wg-net-label">IP</span><span class="wg-net-val">{primaryIface.ip || '—'}</span></div>
                <div class="wg-net-row"><span class="wg-net-label">Velocidad</span><span class="wg-net-val">{primaryIface.speed || '—'}</span></div>
              {:else}
                <div class="wg-empty">Sin conexión</div>
              {/if}
            </div>
          {/if}
        </div>

        <!-- Resize handle -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="wr-handle" on:mousedown={(e) => onResizeStart(e, widget)}></div>
      </div>
    {/each}

    <!-- Add widget button -->
    <div class="widget-add-wrap">
      <button class="widget-add-btn" on:click|stopPropagation={() => showAddMenu = !showAddMenu} title="Añadir widget">+</button>
      {#if showAddMenu}
        <div class="widget-add-menu" on:click|stopPropagation>
          {#each Object.entries(WIDGET_TYPES) as [type, meta]}
            <button class="widget-add-item" on:click={() => addWidget(type)}>
              <span>{meta.icon}</span> {meta.name}
            </button>
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

  .widget-wrap {
    position: absolute;
    pointer-events: auto;
    border-radius: 14px;
    background: rgba(17, 16, 40, 0.55);
    backdrop-filter: blur(18px) saturate(1.3);
    -webkit-backdrop-filter: blur(18px) saturate(1.3);
    border: 1px solid rgba(255,255,255,0.08);
    box-shadow: 0 8px 32px rgba(0,0,0,0.25);
    cursor: grab;
    user-select: none;
    overflow: hidden;
    transition: box-shadow 0.2s;
  }
  .widget-wrap:hover {
    border-color: rgba(255,255,255,0.12);
    box-shadow: 0 8px 32px rgba(0,0,0,0.35);
  }
  .widget-wrap.is-dragging { cursor: grabbing; opacity: 0.9; }
  .widget-wrap.is-resizing { cursor: nwse-resize; }

  :global([data-theme="dark"]) .widget-wrap {
    background: rgba(24,24,24,0.60);
  }
  :global([data-theme="light"]) .widget-wrap {
    background: rgba(235,235,239,0.65);
    border-color: rgba(0,0,0,0.08);
    box-shadow: 0 8px 32px rgba(0,0,0,0.08);
  }

  .wr-close {
    position: absolute; top: 6px; right: 6px;
    width: 18px; height: 18px; border-radius: 50%;
    border: none; background: rgba(255,255,255,0.06);
    color: var(--text-3); font-size: 12px; line-height: 1;
    cursor: pointer; display: flex; align-items: center; justify-content: center;
    opacity: 0; transition: opacity 0.15s;
    z-index: 5;
  }
  .widget-wrap:hover .wr-close { opacity: 1; }
  .wr-close:hover { background: rgba(248,113,113,0.2); color: var(--red); }

  .wr-content {
    width: 100%; height: 100%;
    padding: 14px 16px;
    display: flex; flex-direction: column;
    overflow: hidden;
  }

  .wr-handle {
    position: absolute; bottom: 0; right: 0;
    width: 14px; height: 14px;
    cursor: nwse-resize;
    opacity: 0; transition: opacity 0.15s;
  }
  .widget-wrap:hover .wr-handle { opacity: 0.4; }
  .wr-handle::after {
    content: '';
    position: absolute; bottom: 4px; right: 4px;
    width: 6px; height: 6px;
    border-right: 2px solid var(--text-3);
    border-bottom: 2px solid var(--text-3);
  }

  /* ── CLOCK WIDGET ── */
  .wg-clock {
    display: flex; flex-direction: column;
    justify-content: center; align-items: center;
    height: 100%; gap: 4px;
  }
  .wg-clock-time {
    font-size: 32px; font-weight: 300; letter-spacing: -0.02em;
    color: var(--text-1);
    font-family: 'DM Mono', monospace;
    line-height: 1;
  }
  .wg-clock-date {
    font-size: 11px; color: var(--text-3);
    text-transform: capitalize;
  }

  /* ── SYSMON WIDGET ── */
  .wg-sysmon { display: flex; flex-direction: column; height: 100%; gap: 8px; }
  .wg-sysmon-title {
    font-size: 10px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: 0.06em;
  }
  .wg-bar-group { display: flex; flex-direction: column; gap: 8px; flex: 1; }
  .wg-bar-row { display: flex; align-items: center; gap: 8px; }
  .wg-bar-label { font-size: 10px; font-weight: 600; color: var(--text-2); width: 28px; flex-shrink: 0; }
  .wg-bar-track {
    flex: 1; height: 6px; border-radius: 3px;
    background: rgba(255,255,255,0.06);
    overflow: hidden;
  }
  :global([data-theme="light"]) .wg-bar-track { background: rgba(0,0,0,0.06); }
  .wg-bar-fill {
    height: 100%; border-radius: 3px;
    transition: width 0.6s ease;
  }
  .wg-bar-fill.cpu { background: linear-gradient(90deg, var(--accent), var(--accent2, #c054f0)); }
  .wg-bar-fill.ram { background: linear-gradient(90deg, #60a5fa, #818cf8); }
  .wg-bar-fill.storage { background: linear-gradient(90deg, #4ade80, #22d3ee); }
  .wg-bar-val { font-size: 10px; color: var(--text-2); font-family: 'DM Mono', monospace; width: 30px; text-align: right; flex-shrink: 0; }
  .wg-sysmon-footer {
    display: flex; gap: 12px;
    font-size: 10px; color: var(--text-3);
    font-family: 'DM Mono', monospace;
  }

  /* ── STORAGE WIDGET ── */
  .wg-storage { display: flex; flex-direction: column; height: 100%; gap: 6px; }
  .wg-storage-title {
    font-size: 10px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: 0.06em;
  }
  .wg-pool-row { display: flex; align-items: center; gap: 8px; }
  .wg-pool-name { font-size: 11px; font-weight: 600; color: var(--text-1); width: 60px; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .wg-pool-detail { font-size: 9px; color: var(--text-3); font-family: 'DM Mono', monospace; margin-left: 68px; }

  /* ── NETWORK WIDGET ── */
  .wg-network { display: flex; flex-direction: column; height: 100%; gap: 5px; }
  .wg-network-title {
    font-size: 10px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: 0.06em;
    margin-bottom: 2px;
  }
  .wg-net-row { display: flex; justify-content: space-between; align-items: center; }
  .wg-net-label { font-size: 10px; color: var(--text-3); }
  .wg-net-val { font-size: 11px; color: var(--text-1); font-family: 'DM Mono', monospace; }

  .wg-empty { font-size: 11px; color: var(--text-3); flex: 1; display: flex; align-items: center; }

  /* ── ADD WIDGET ── */
  .widget-add-wrap {
    position: fixed; bottom: 68px; right: 16px;
    z-index: 2; pointer-events: auto;
  }
  .widget-add-btn {
    width: 36px; height: 36px; border-radius: 50%;
    border: 1px solid rgba(255,255,255,0.1);
    background: rgba(17,16,40,0.6);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    color: var(--text-2); font-size: 20px; line-height: 1;
    cursor: pointer; display: flex; align-items: center; justify-content: center;
    transition: all 0.15s;
  }
  .widget-add-btn:hover { background: rgba(124,111,255,0.2); color: var(--text-1); border-color: var(--accent); }

  :global([data-theme="light"]) .widget-add-btn {
    background: rgba(235,235,239,0.7);
    border-color: rgba(0,0,0,0.1);
  }

  .widget-add-menu {
    position: absolute; bottom: 44px; right: 0;
    min-width: 160px;
    background: var(--bg-inner);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 12px 36px rgba(0,0,0,0.4);
    overflow: hidden;
    animation: menuIn 0.12s ease both;
  }
  @keyframes menuIn { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: translateY(0); } }

  .widget-add-item {
    display: flex; align-items: center; gap: 8px;
    width: 100%; padding: 9px 14px;
    border: none; background: none;
    font-size: 12px; color: var(--text-2);
    cursor: pointer; font-family: inherit;
    transition: all 0.1s;
    text-align: left;
  }
  .widget-add-item:hover { background: var(--active-bg); color: var(--text-1); }
</style>
