<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  let torrents = [];
  let activeTab = 'all'; // 'all' | 'active' | 'done' | 'stopped'
  let loading = true;
  let pollInterval;

  async function fetchTorrents() {
    try {
      const r = await fetch('/api/torrent/torrents', { headers: hdrs() });
      const d = await r.json();
      torrents = Array.isArray(d) ? d : (d.torrents || []);
    } catch { torrents = []; }
    loading = false;
  }

  onMount(() => {
    fetchTorrents();
    pollInterval = setInterval(fetchTorrents, 4000);
  });
  onDestroy(() => clearInterval(pollInterval));

  // Filters
  $: active  = torrents.filter(t => t.status === 'downloading' || t.progress < 100);
  $: done    = torrents.filter(t => t.status === 'seeding'     || t.progress >= 100);
  $: stopped = torrents.filter(t => t.status === 'paused'      || t.status === 'stopped');

  $: filtered = activeTab === 'all'     ? torrents
              : activeTab === 'active'  ? active
              : activeTab === 'done'    ? done
              : stopped;

  // Upload .torrent
  function addTorrent() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.torrent';
    input.onchange = async (e) => {
      const file = e.target.files[0];
      if (!file) return;
      const fd = new FormData();
      fd.append('torrent', file);
      await fetch('/api/torrent/upload', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${getToken()}` },
        body: fd,
      });
      fetchTorrents();
    };
    input.click();
  }

  async function pauseTorrent(hash) {
    await fetch(`/api/torrent/pause/${hash}`, { method: 'POST', headers: hdrs() });
    fetchTorrents();
  }

  async function resumeTorrent(hash) {
    await fetch(`/api/torrent/resume/${hash}`, { method: 'POST', headers: hdrs() });
    fetchTorrents();
  }

  async function deleteTorrent(hash) {
    await fetch(`/api/torrent/delete/${hash}`, { method: 'POST', headers: hdrs() });
    fetchTorrents();
  }

  // Formatting
  function fmtSize(bytes) {
    if (!bytes) return '—';
    if (bytes >= 1e12) return (bytes/1e12).toFixed(1) + ' TB';
    if (bytes >= 1e9)  return (bytes/1e9).toFixed(1)  + ' GB';
    if (bytes >= 1e6)  return (bytes/1e6).toFixed(1)  + ' MB';
    return (bytes/1e3).toFixed(0) + ' KB';
  }

  function fmtSpeed(bytes) {
    if (!bytes || bytes < 100) return '';
    if (bytes >= 1e6) return (bytes/1e6).toFixed(1) + ' MB/s';
    return (bytes/1e3).toFixed(0) + ' KB/s';
  }

  function isDownloading(t) { return t.status === 'downloading' || (t.progress < 100 && t.status !== 'paused' && t.status !== 'stopped'); }
  function isPaused(t)      { return t.status === 'paused' || t.status === 'stopped'; }
  function isDone(t)        { return t.status === 'seeding' || t.progress >= 100; }

  // Stats for statusbar
  $: dlSpeed = torrents.reduce((a, t) => a + (t.dlSpeed || t.downloadSpeed || 0), 0);
  $: ulSpeed = torrents.reduce((a, t) => a + (t.ulSpeed || t.uploadSpeed   || 0), 0);
</script>

<div class="nt-root">

  <!-- SIDEBAR -->
  <div class="sidebar">
    <div class="sb-header">
      <div class="sb-logo-wrap">
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
          <path d="M7 1v9M3.5 7l3.5 4 3.5-4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <div class="sb-logo-line"></div>
      </div>
      <span class="sb-title">NimTorrent</span>
    </div>

    <div class="sb-search">⌕ Buscar…</div>

    <div class="sb-section">Vistas</div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'all'} on:click={() => activeTab = 'all'}>
      <span class="sb-ico">⊟</span> Panel
      {#if torrents.length > 0}<span class="sb-badge">{torrents.length}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'active'} on:click={() => activeTab = 'active'}>
      <span class="sb-ico">↓</span> Descargas
      {#if active.length > 0}<span class="sb-badge blue">{active.length}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'done'} on:click={() => activeTab = 'done'}>
      <span class="sb-ico">✓</span> Completados
      {#if done.length > 0}<span class="sb-badge green">{done.length}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'stopped'} on:click={() => activeTab = 'stopped'}>
      <span class="sb-ico">⏹</span> Parados
      {#if stopped.length > 0}<span class="sb-badge">{stopped.length}</span>{/if}
    </div>

    <div class="sb-section" style="margin-top:8px">Trackers</div>
    <div class="sb-item">
      <span class="sb-ico">⬡</span> Todos los trackers
    </div>
  </div>

  <!-- INNER -->
  <div class="inner-wrap">
    <div class="inner">

      <!-- TITLEBAR -->
      <div class="inner-titlebar">
        <!-- Tabs -->
        <div class="tabs">
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tab" class:active-tab={activeTab === 'all'} on:click={() => activeTab = 'all'}>
            <div class="tab-dot"></div>
            Activos
            {#if active.length > 0}<span class="tab-count">{active.length}</span>{/if}
          </div>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tab done-tab" class:active={activeTab === 'done'} on:click={() => activeTab = 'done'}>
            <div class="tab-dot"></div>
            Finalizado
            {#if done.length > 0}<span class="tab-count">{done.length}</span>{/if}
          </div>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tab stopped-tab" class:active={activeTab === 'stopped'} on:click={() => activeTab = 'stopped'}>
            <div class="tab-dot"></div>
            Parado
            {#if stopped.length > 0}<span class="tab-count">{stopped.length}</span>{/if}
          </div>
        </div>

        <!-- Actions -->
        <div class="tb-actions">
          <button class="tb-btn" title="Pausar todo">⏸</button>
          <button class="tb-btn" title="Parar todo">⏹</button>
          <button class="btn-accent" on:click={addTorrent}>↑ Añadir</button>
        </div>
      </div>

      <!-- TORRENT LIST -->
      <div class="torrent-list">
        {#if loading}
          <div class="t-empty"><div class="spinner"></div></div>
        {:else if filtered.length === 0}
          <div class="t-empty">
            <div class="t-empty-icon">⬇</div>
            <div>Sin torrents</div>
            <button class="btn-accent" style="margin-top:10px" on:click={addTorrent}>Añadir torrent</button>
          </div>
        {:else}
          {#each filtered as t (t.hash || t.id)}
            <div class="torrent-row">
              <div class="t-name">{t.name}</div>
              <div class="t-progress">
                <div class="t-bar-bg">
                  <div class="t-bar-fill" style="width:{t.progress ?? 0}%"
                    class:done={isDone(t)} class:paused={isPaused(t)}></div>
                </div>
                <span class="t-pct">{(t.progress ?? 0).toFixed(0)}%</span>
              </div>
              <div class="t-stats">
                <span class="t-stat">{fmtSize(t.downloaded || 0)} / {fmtSize(t.size || t.totalSize)}</span>
                {#if isDownloading(t)}
                  <span class="t-stat down">↓ {fmtSpeed(t.dlSpeed || t.downloadSpeed) || '0 KB/s'}</span>
                {/if}
                {#if isDone(t) || (isDownloading(t) && (t.ulSpeed || t.uploadSpeed))}
                  <span class="t-stat up">↑ {fmtSpeed(t.ulSpeed || t.uploadSpeed) || '0 KB/s'}</span>
                {/if}
              </div>
              <div class="t-actions">
                {#if isPaused(t)}
                  <button class="t-action" title="Reanudar" on:click={() => resumeTorrent(t.hash || t.id)}>▶</button>
                {:else}
                  <button class="t-action" title="Pausar" on:click={() => pauseTorrent(t.hash || t.id)}>⏸</button>
                {/if}
                <button class="t-action" title="Más opciones">···</button>
              </div>
            </div>
          {/each}
        {/if}
      </div>

      <!-- STATUSBAR -->
      <div class="statusbar">
        <div class="status-dot"></div>
        <span>{torrents.length} torrents</span>
        <div class="status-sep"></div>
        {#if dlSpeed > 100}
          <span>↓ {fmtSpeed(dlSpeed)}</span>
          <div class="status-sep"></div>
        {/if}
        {#if ulSpeed > 100}
          <span>↑ {fmtSpeed(ulSpeed)}</span>
          <div class="status-sep"></div>
        {/if}
        <span style="margin-left:auto">{active.length} activos · {done.length} completados</span>
      </div>

    </div>
  </div>
</div>

<style>
  .nt-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'DM Sans',sans-serif; color:var(--text-1); }

  /* ── SIDEBAR ── */
  .sidebar {
    width:190px; flex-shrink:0;
    display:flex; flex-direction:column;
    padding:12px 8px;
    background:var(--bg-sidebar);
  }
  .sb-header { display:flex; flex-direction:row; align-items:center; gap:9px; padding:32px 8px 20px; }
  .sb-logo-wrap { display:flex; flex-direction:column; align-items:center; gap:3px; flex-shrink:0; color:var(--text-1); }
  .sb-logo-arrow { font-size:15px; font-weight:900; color:var(--text-1); line-height:1; }
  .sb-logo-line { width:16px; height:3px; border-radius:2px; background:rgba(255,255,255,0.55); }
  .sb-title { font-size:14px; font-weight:700; color:var(--text-1); }
  .sb-search {
    display:flex; align-items:center; gap:6px;
    padding:4px 10px; border-radius:8px; margin-bottom:10px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    font-size:11px; color:var(--text-3); cursor:text;
  }
  .sb-section { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; padding:0 10px 4px; margin-top:4px; }
  .sb-item {
    display:flex; align-items:center; gap:8px;
    padding:7px 10px; border-radius:8px; cursor:pointer;
    font-size:12px; color:var(--text-2);
    border:1px solid transparent; transition:all .15s;
  }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-ico { font-size:12px; width:14px; text-align:center; flex-shrink:0; }
  .sb-badge {
    margin-left:auto; padding:1px 6px; border-radius:10px;
    font-size:9px; font-weight:700; font-family:'DM Mono',monospace;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3);
  }
  .sb-badge.blue  { background:rgba(96,165,250,0.12); border-color:rgba(96,165,250,0.25); color:rgba(96,165,250,0.9); }
  .sb-badge.green { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.22); color:rgba(74,222,128,0.85); }

  /* ── INNER ── */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner {
    flex:1; border-radius:10px; border:1px solid var(--border);
    background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden;
  }

  /* ── TITLEBAR ── */
  .inner-titlebar {
    display:flex; align-items:center; gap:8px;
    padding:10px 14px 9px; background:var(--bg-bar); flex-shrink:0;
  }
  .tabs { display:flex; gap:4px; flex:1; }
  .tab {
    display:flex; align-items:center; gap:5px;
    padding:5px 10px; border-radius:6px; cursor:pointer;
    font-size:11px; font-weight:500; color:var(--text-3);
    border:1px solid transparent; transition:all .15s;
  }
  .tab:hover { color:var(--text-2); }
  .tab.active-tab {
    background:rgba(96,165,250,0.10); border-color:rgba(96,165,250,0.30);
    color:rgba(96,165,250,0.90);
  }
  .tab.done-tab.active  {
    background:rgba(74,222,128,0.08); border-color:rgba(74,222,128,0.25);
    color:rgba(74,222,128,0.85);
  }
  .tab.stopped-tab.active {
    background:rgba(148,163,184,0.08); border-color:rgba(148,163,184,0.22);
    color:rgba(148,163,184,0.75);
  }
  .tab-dot {
    width:5px; height:5px; border-radius:50%;
    background:rgba(128,128,128,0.3);
  }
  .active-tab  .tab-dot { background:rgba(96,165,250,0.90);  box-shadow:0 0 4px rgba(96,165,250,0.6); }
  .done-tab.active   .tab-dot { background:rgba(74,222,128,0.85);  box-shadow:0 0 4px rgba(74,222,128,0.5); }
  .stopped-tab.active .tab-dot { background:rgba(148,163,184,0.70); }
  .tab-count {
    font-size:9px; font-weight:700; padding:1px 5px; border-radius:8px;
    background:rgba(255,255,255,0.07); color:var(--text-3);
    font-family:'DM Mono',monospace;
  }
  .tb-actions { display:flex; align-items:center; gap:6px; }
  .tb-btn {
    width:34px; height:34px; border-radius:8px;
    border:none; background:transparent;
    color:var(--text-2); cursor:pointer; font-size:15px;
    display:flex; align-items:center; justify-content:center;
    transition:all .15s;
  }
  .tb-btn:hover { color:var(--text-1); background:rgba(128,128,128,0.10); }
  .btn-accent {
    padding:5px 12px; border-radius:7px; border:none; cursor:pointer;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    color:#fff; font-size:11px; font-weight:600; font-family:inherit;
    transition:all .15s;
  }
  .btn-accent:hover { opacity:.88; transform:translateY(-1px); }

  /* ── TORRENT LIST ── */
  .torrent-list { flex:1; overflow-y:auto; padding:8px; }
  .torrent-list::-webkit-scrollbar { width:3px; }
  .torrent-list::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .t-empty {
    height:100%; display:flex; flex-direction:column;
    align-items:center; justify-content:center;
    gap:8px; color:var(--text-3); font-size:12px;
  }
  .t-empty-icon { font-size:28px; opacity:.3; }

  .torrent-row {
    display:flex; align-items:center; gap:6px;
    padding:7px 12px; border-radius:8px; margin-bottom:2px;
    border:1px solid transparent; transition:all .15s;
    animation:fadeUp .25s ease both;
  }
  .torrent-row:hover { background:rgba(128,128,128,0.06); border-color:var(--border); }
  @keyframes fadeUp { from{opacity:0;transform:translateY(4px)} to{opacity:1;transform:none} }

  .t-name {
    font-size:12px; font-weight:500; color:var(--text-1);
    white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
    min-width:120px; max-width:190px; flex-shrink:0;
  }

  .t-progress { display:flex; align-items:center; gap:6px; flex:1; min-width:0; }
  .t-bar-bg { flex:1; max-width:220px; height:3px; background:rgba(128,128,128,0.15); border-radius:2px; overflow:hidden; }
  .t-bar-fill { height:100%; border-radius:2px; background:linear-gradient(90deg, var(--accent), var(--accent2)); transition:width .4s; }
  .t-bar-fill.done   { background:linear-gradient(90deg, #4ade80, #22d3ee); }
  .t-bar-fill.paused { background:rgba(128,128,128,0.3); }
  .t-pct  { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex-shrink:0; width:28px; text-align:right; }

  .t-stats { display:flex; align-items:center; gap:5px; flex-shrink:0; }
  .t-stat { display:flex; align-items:center; gap:3px; font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; white-space:nowrap; }
  .t-stat.down { color:#4ade80; }
  .t-stat.up   { color:#60a5fa; }

  .t-actions { display:flex; gap:4px; flex-shrink:0; }
  .t-action {
    width:24px; height:24px; border-radius:6px; border:none;
    background:transparent; color:var(--text-2); cursor:pointer;
    font-size:11px; display:flex; align-items:center; justify-content:center;
    transition:all .15s; font-family:inherit;
  }
  .t-action:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .t-action.danger:hover { background:rgba(248,113,113,0.12); color:var(--red); }

  /* ── STATUSBAR ── */
  .statusbar {
    display:flex; align-items:center; gap:10px;
    padding:7px 14px; border-top:1px solid var(--border);
    background:var(--bg-bar); flex-shrink:0;
    font-size:10px; color:var(--text-3);
    border-radius:0 0 10px 10px; font-family:'DM Mono',monospace;
  }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,0.6); flex-shrink:0; }
  .status-sep { width:1px; height:10px; background:var(--border); }

  .spinner {
    width:24px; height:24px; border-radius:50%;
    border:2px solid rgba(255,255,255,0.08);
    border-top-color:var(--accent);
    animation:spin .7s linear infinite;
  }
  @keyframes spin { to { transform:rotate(360deg); } }
</style>
