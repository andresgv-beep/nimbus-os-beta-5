<script>
  import { onMount, onDestroy, tick } from 'svelte';
  import { prefs, setPref } from '$lib/stores/theme.js';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  const CELL = 80;
  const GAP = 10;
  const MARGIN = 16;

  const WIDGET_TYPES = {
    clock:   { name: 'Reloj',     icon: '🕐', cols: 3, rows: 2 },
    sysmon:  { name: 'Sistema',   icon: '📊', cols: 3, rows: 3 },
    storage: { name: 'Storage',   icon: '💾', cols: 3, rows: 2 },
    network: { name: 'Red',       icon: '🌐', cols: 3, rows: 2 },
  };

  let widgets = [];
  let gridCols = 0;
  let gridRows = 0;
  let areaEl;
  let layoutLoaded = false;

  const DEFAULT_LAYOUT = [
    { id: 'w1', type: 'clock',   col: 0, row: 0, cols: 3, rows: 2 },
    { id: 'w2', type: 'sysmon',  col: 0, row: 2, cols: 3, rows: 3 },
    { id: 'w3', type: 'storage', col: -3, row: 0, cols: 3, rows: 2 },
    { id: 'w4', type: 'network', col: -3, row: 2, cols: 3, rows: 2 },
  ];

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
      let col = w.col;
      if (col < 0) col = gridCols + col;
      col = Math.max(0, Math.min(gridCols - w.cols, col));
      const row = Math.max(0, Math.min(gridRows - w.rows, w.row));
      return { ...w, col, row };
    });
  }

  function loadLayout() {
    const saved = $prefs.widgetLayout;
    if (saved && Array.isArray(saved) && saved.length > 0) {
      widgets = resolveLayout(saved);
    } else {
      widgets = resolveLayout(DEFAULT_LAYOUT);
    }
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
    if (e.target.closest('.wr-close')) return;
    e.preventDefault();
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
      if (idx >= 0) { widgets[idx].col = dragPreviewCol; widgets[idx].row = dragPreviewRow; widgets = widgets; saveLayout(); }
    }
    dragging = null; dragWidget = null;
    window.removeEventListener('mousemove', onDragMove);
    window.removeEventListener('mouseup', onDragEnd);
  }

  function removeWidget(id) { widgets = widgets.filter(w => w.id !== id); saveLayout(); }

  let showAddMenu = false;
  function addWidget(type) {
    const meta = WIDGET_TYPES[type];
    const id = 'w' + Date.now();
    let placed = false;
    for (let r = 0; r <= gridRows - meta.rows && !placed; r++) {
      for (let c = 0; c <= gridCols - meta.cols && !placed; c++) {
        if (!widgets.some(w => c < w.col + w.cols && c + meta.cols > w.col && r < w.row + w.rows && r + meta.rows > w.row)) {
          widgets = [...widgets, { id, type, col: c, row: r, cols: meta.cols, rows: meta.rows }]; placed = true;
        }
      }
    }
    if (!placed) widgets = [...widgets, { id, type, col: 0, row: 0, cols: meta.cols, rows: meta.rows }];
    showAddMenu = false; saveLayout();
  }

  // ── Data ──
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
      sysData = sys || {}; storageData = stor || {}; netData = net || {};
    } catch {}
  }
  function startPolling() { fetchData(); pollTimer = setInterval(fetchData, 5000); }
  function stopPolling() { if (pollTimer) clearInterval(pollTimer); }

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
    if (b >= 1e9) return (b / 1e9).toFixed(1) + ' GB';
    return (b / 1e6).toFixed(0) + ' MB';
  }

  $: cpuPct = sysData.cpu?.percent ?? sysData.cpuPercent ?? 0;
  $: memPct = sysData.memory?.percent ?? sysData.memPercent ?? 0;
  $: memUsed = sysData.memory?.used ?? 0;
  $: memTotal = sysData.memory?.total ?? 0;
  $: cpuTemp = sysData.temps?.cpu ?? sysData.cpuTemp ?? null;
  $: pools = storageData.pools || [];
  $: netIfaces = Array.isArray(netData) ? netData : (netData.interfaces || []);
  $: primaryIface = netIfaces.find(i => i.ip && i.ip !== '127.0.0.1') || netIfaces[0] || {};
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="widget-layer" bind:this={areaEl} on:click={() => showAddMenu = false}>
  {#if $prefs.showWidgets}
    {#each widgets as widget (widget.id)}
      {@const isDrag = dragging?.id === widget.id}
      {@const dc = isDrag ? dragPreviewCol : widget.col}
      {@const dr = isDrag ? dragPreviewRow : widget.row}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="wc" class:dragging={isDrag}
        style="left:{cellX(dc)}px;top:{cellY(dr)}px;width:{cellW(widget.cols)}px;height:{cellH(widget.rows)}px;"
        on:mousedown={(e) => onDragStart(e, widget)}>
        <button class="wc-x" on:click|stopPropagation={() => removeWidget(widget.id)}>×</button>
        <div class="wc-in">
          {#if widget.type === 'clock'}
            <div class="wg-clock"><div class="wg-clock-t">{clockTime}</div><div class="wg-clock-d">{clockDate}</div></div>
          {:else if widget.type === 'sysmon'}
            <div class="wg-col"><div class="wg-h">Sistema</div>
              <div class="wg-bars">
                <div class="wg-br"><span class="wg-bl">CPU</span><div class="wg-bt"><div class="wg-bf cpu" style="width:{Math.min(100,cpuPct)}%"></div></div><span class="wg-bv">{cpuPct.toFixed(0)}%</span></div>
                <div class="wg-br"><span class="wg-bl">RAM</span><div class="wg-bt"><div class="wg-bf ram" style="width:{Math.min(100,memPct)}%"></div></div><span class="wg-bv">{memPct.toFixed(0)}%</span></div>
              </div>
              <div class="wg-ft">{#if cpuTemp}<span>🌡 {cpuTemp}°C</span>{/if}<span>{fmtBytes(memUsed)} / {fmtBytes(memTotal)}</span></div>
            </div>
          {:else if widget.type === 'storage'}
            <div class="wg-col"><div class="wg-h">Storage</div>
              {#if pools.length > 0}
                {#each pools.slice(0,2) as pool}
                  <div class="wg-br"><span class="wg-bl" style="width:50px">{pool.name}</span><div class="wg-bt"><div class="wg-bf sto" style="width:{pool.usagePercent||0}%"></div></div><span class="wg-bv">{pool.usagePercent||0}%</span></div>
                  <div class="wg-sub">{pool.usedFormatted||'—'} / {pool.totalFormatted||'—'}</div>
                {/each}
              {:else}<div class="wg-em">Sin pools</div>{/if}
            </div>
          {:else if widget.type === 'network'}
            <div class="wg-col"><div class="wg-h">Red</div>
              {#if primaryIface.name}
                <div class="wg-kv"><span>Interfaz</span><span>{primaryIface.name}</span></div>
                <div class="wg-kv"><span>IP</span><span>{primaryIface.ip||'—'}</span></div>
                <div class="wg-kv"><span>Velocidad</span><span>{primaryIface.speed||'—'}</span></div>
              {:else}<div class="wg-em">Sin conexión</div>{/if}
            </div>
          {/if}
        </div>
      </div>
      {#if isDrag}
        <div class="snap-ghost" style="left:{cellX(dragPreviewCol)}px;top:{cellY(dragPreviewRow)}px;width:{cellW(widget.cols)}px;height:{cellH(widget.rows)}px;"></div>
      {/if}
    {/each}

    <div class="wa-wrap">
      <button class="wa-btn" on:click|stopPropagation={() => showAddMenu = !showAddMenu}>+</button>
      {#if showAddMenu}
        <div class="wa-menu" on:click|stopPropagation>
          {#each Object.entries(WIDGET_TYPES) as [type, meta]}
            <button class="wa-item" on:click={() => addWidget(type)}><span>{meta.icon}</span> {meta.name}</button>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .widget-layer { position:fixed; inset:0; z-index:1; pointer-events:none; }

  .wc {
    position:absolute; pointer-events:auto; border-radius:14px;
    background:rgba(17,16,40,0.50); backdrop-filter:blur(18px) saturate(1.3);
    -webkit-backdrop-filter:blur(18px) saturate(1.3);
    border:1px solid rgba(255,255,255,0.07); box-shadow:0 4px 20px rgba(0,0,0,0.20);
    cursor:grab; user-select:none; overflow:hidden;
    transition:left .22s cubic-bezier(.25,1,.5,1), top .22s cubic-bezier(.25,1,.5,1), box-shadow .2s;
  }
  .wc:hover { border-color:rgba(255,255,255,0.12); box-shadow:0 6px 24px rgba(0,0,0,0.30); }
  .wc.dragging { cursor:grabbing; opacity:.85; transition:none; z-index:10; box-shadow:0 12px 40px rgba(0,0,0,0.40); }
  :global([data-theme="dark"]) .wc { background:rgba(24,24,24,0.55); }
  :global([data-theme="light"]) .wc { background:rgba(235,235,239,0.60); border-color:rgba(0,0,0,0.07); box-shadow:0 4px 20px rgba(0,0,0,0.06); }

  .snap-ghost { position:absolute; border-radius:14px; border:2px dashed var(--accent); background:rgba(124,111,255,0.06); pointer-events:none; z-index:0; transition:left .15s ease, top .15s ease; }

  .wc-x {
    position:absolute; top:6px; right:6px; width:18px; height:18px; border-radius:50%;
    border:none; background:rgba(255,255,255,0.06); color:var(--text-3); font-size:12px;
    cursor:pointer; display:flex; align-items:center; justify-content:center;
    opacity:0; transition:opacity .15s; z-index:5;
  }
  .wc:hover .wc-x { opacity:1; }
  .wc-x:hover { background:rgba(248,113,113,0.2); color:var(--red); }

  .wc-in { width:100%; height:100%; padding:14px 16px; display:flex; flex-direction:column; overflow:hidden; }

  /* Widgets */
  .wg-h { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:8px; }
  .wg-col { display:flex; flex-direction:column; height:100%; gap:4px; }
  .wg-ft { display:flex; gap:12px; margin-top:auto; font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .wg-sub { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; margin:1px 0 3px 58px; }
  .wg-em { font-size:11px; color:var(--text-3); flex:1; display:flex; align-items:center; }
  .wg-kv { display:flex; justify-content:space-between; padding:2px 0; }
  .wg-kv span:first-child { font-size:10px; color:var(--text-3); }
  .wg-kv span:last-child { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; }

  .wg-bars { display:flex; flex-direction:column; gap:8px; flex:1; }
  .wg-br { display:flex; align-items:center; gap:8px; }
  .wg-bl { font-size:10px; font-weight:600; color:var(--text-2); width:28px; flex-shrink:0; }
  .wg-bt { flex:1; height:6px; border-radius:3px; background:rgba(255,255,255,0.06); overflow:hidden; }
  :global([data-theme="light"]) .wg-bt { background:rgba(0,0,0,0.06); }
  .wg-bf { height:100%; border-radius:3px; transition:width .6s ease; }
  .wg-bf.cpu { background:linear-gradient(90deg, var(--accent), var(--accent2,#c054f0)); }
  .wg-bf.ram { background:linear-gradient(90deg, #60a5fa, #818cf8); }
  .wg-bf.sto { background:linear-gradient(90deg, #4ade80, #22d3ee); }
  .wg-bv { font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; width:30px; text-align:right; flex-shrink:0; }

  .wg-clock { display:flex; flex-direction:column; justify-content:center; align-items:center; height:100%; gap:4px; }
  .wg-clock-t { font-size:32px; font-weight:300; letter-spacing:-.02em; color:var(--text-1); font-family:'DM Mono',monospace; line-height:1; }
  .wg-clock-d { font-size:11px; color:var(--text-3); text-transform:capitalize; }

  /* Add */
  .wa-wrap { position:fixed; bottom:68px; right:16px; z-index:2; pointer-events:auto; }
  .wa-btn {
    width:36px; height:36px; border-radius:50%; border:1px solid rgba(255,255,255,0.08);
    background:rgba(17,16,40,0.5); backdrop-filter:blur(12px); -webkit-backdrop-filter:blur(12px);
    color:var(--text-2); font-size:20px; cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:all .15s;
  }
  .wa-btn:hover { background:rgba(124,111,255,0.2); color:var(--text-1); border-color:var(--accent); }
  :global([data-theme="light"]) .wa-btn { background:rgba(235,235,239,0.65); border-color:rgba(0,0,0,0.08); }

  .wa-menu {
    position:absolute; bottom:44px; right:0; min-width:160px;
    background:var(--bg-inner); border:1px solid var(--border); border-radius:10px;
    box-shadow:0 12px 36px rgba(0,0,0,0.4); overflow:hidden;
    animation:menuIn .12s ease both;
  }
  @keyframes menuIn { from{opacity:0;transform:translateY(4px)} to{opacity:1;transform:translateY(0)} }
  .wa-item {
    display:flex; align-items:center; gap:8px; width:100%; padding:9px 14px;
    border:none; background:none; font-size:12px; color:var(--text-2);
    cursor:pointer; font-family:inherit; transition:all .1s; text-align:left;
  }
  .wa-item:hover { background:var(--active-bg); color:var(--text-1); }
</style>
