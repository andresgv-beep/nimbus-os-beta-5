<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  let shares = [];
  let currentShare = null;
  let currentPath = '/';
  let files = [];
  let loading = false;
  let selected = new Set();
  let storageInfo = null;

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' });

  async function fetchShares() {
    try { const r = await fetch('/api/files', { headers: hdrs() }); const d = await r.json(); if (d.shares) shares = d.shares; } catch {}
  }
  async function fetchFiles() {
    if (!currentShare) { files = []; return; }
    loading = true;
    try { const r = await fetch(`/api/files?share=${currentShare}&path=${encodeURIComponent(currentPath)}`, { headers: hdrs() }); const d = await r.json(); files = d.files || []; } catch { files = []; }
    selected = new Set(); loading = false;
  }
  async function fetchStorage() {
    try { const r = await fetch('/api/storage/status', { headers: hdrs() }); const d = await r.json(); if (d.pools?.length) storageInfo = d.pools[0]; } catch {}
  }

  onMount(() => { fetchShares(); fetchStorage(); });
  $: if (currentShare !== undefined) fetchFiles();

  function navigate(share, path) { currentShare = share; currentPath = path; }
  function goBack() {
    if (currentPath !== '/') currentPath = currentPath.split('/').slice(0, -1).join('/') || '/';
    else if (currentShare) { currentShare = null; currentPath = '/'; }
  }
  function openItem(file) {
    if (file.isDirectory) { currentPath = currentPath === '/' ? `/${file.name}` : `${currentPath}/${file.name}`; return; }
    const fp = currentPath === '/' ? `/${file.name}` : `${currentPath}/${file.name}`;
    window.open(`/api/files/download?share=${currentShare}&path=${encodeURIComponent(fp)}&token=${getToken()}`, '_blank');
  }
  function toggleSelect(i, e) {
    if (e?.ctrlKey || e?.metaKey) { const n = new Set(selected); n.has(i) ? n.delete(i) : n.add(i); selected = n; }
    else selected = new Set([i]);
  }
  function uploadFiles() {
    const input = document.createElement('input'); input.type = 'file'; input.multiple = true;
    input.onchange = async (e) => {
      for (const f of e.target.files) {
        const fd = new FormData(); fd.append('file', f); fd.append('share', currentShare); fd.append('path', currentPath);
        await fetch('/api/files/upload', { method: 'POST', headers: { 'Authorization': `Bearer ${getToken()}` }, body: fd });
      }
      fetchFiles();
    };
    input.click();
  }

  $: sorted = [...files].sort((a,b) => (a.isDirectory?-1:1) - (b.isDirectory?-1:1) || a.name.localeCompare(b.name));
  $: shareInfo = shares.find(s => s.name === currentShare);
  $: pathParts = currentPath === '/' ? [] : currentPath.split('/').filter(Boolean);

  function fIcon(file) {
    if (file.isDirectory) return '📁';
    const e = file.name.split('.').pop().toLowerCase();
    return {mp4:'🎬',mkv:'🎬',avi:'🎬',mov:'🎬',mp3:'🎵',wav:'🎵',flac:'🎵',jpg:'🖼️',jpeg:'🖼️',png:'🖼️',gif:'🖼️',svg:'🎨',pdf:'📕',doc:'📝',zip:'📦',rar:'📦',js:'💻',py:'💻',go:'💻',txt:'📄',md:'📄',json:'📄',html:'📄',css:'🅰',iso:'💿',sh:'🔧'}[e] || '📄';
  }
  function fDate(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    return `${String(d.getDate()).padStart(2,'0')}/${String(d.getMonth()+1).padStart(2,'0')} ${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`;
  }
</script>

<div class="files-root">
  <!-- SIDEBAR -->
  <div class="sidebar">
    <div class="sb-header">
      <span style="font-size:17px">⠿</span>
      <span class="title">Files</span>
    </div>

    <div class="sb-item search"><span class="ico">⌕</span>Search</div>
    <div class="sb-item"><span class="ico">⇄</span>Shared</div>

    <div class="sb-section">Starred</div>
    {#each shares as share}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={currentShare === share.name}
        on:click={() => navigate(share.name, '/')}>
        <span class="ico">◧</span>{share.displayName || share.name}
      </div>
    {/each}

    {#if storageInfo}
      <div class="sb-storage">
        <div class="ss-labels"><span>{storageInfo.name}</span><strong>{storageInfo.totalFormatted}</strong></div>
        <div class="ss-bar"><div class="ss-fill" style="width:{storageInfo.usagePercent}%"></div></div>
      </div>
    {/if}
  </div>

  <!-- INNER WRAP -->
  <div class="inner-wrap">
    <div class="inner">
      <!-- TITLEBAR -->
      <div class="inner-titlebar">
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <button class="nav-btn" on:click={goBack}>‹</button>
        <button class="nav-btn">›</button>
        <span class="inner-title">
          {#if !currentShare}Shared Folders{:else}{shareInfo?.displayName || currentShare} <small>{sorted.length} items</small>{/if}
        </span>
        <div class="tb-right">
          {#if currentShare}
            <div class="icon-btn">📁</div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="icon-btn" on:click={uploadFiles}>⬆</div>
          {/if}
          <button class="btn-import" on:click={uploadFiles}>⬆ Import</button>
        </div>
      </div>

      <!-- FILE GRID -->
      <div class="file-grid">
        {#if !currentShare}
          {#each shares as share, i}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="f-item" on:dblclick={() => navigate(share.name, '/')}>
              <div class="f-icon">📁</div>
              <div class="f-name">{share.displayName || share.name}</div>
            </div>
          {/each}
        {:else if loading}
          <div style="grid-column:1/-1;text-align:center;padding:40px;color:var(--text-3);font-size:13px">Loading...</div>
        {:else}
          {#each sorted as file, i}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="f-item" class:sel={selected.has(i)}
              on:click={(e) => toggleSelect(i, e)}
              on:dblclick={() => openItem(file)}>
              <div class="f-icon">{fIcon(file)}</div>
              <div class="f-name">{file.name}</div>
              <div class="f-date">{fDate(file.modified)}</div>
            </div>
          {/each}
          {#if sorted.length === 0}
            <div style="grid-column:1/-1;text-align:center;padding:40px;color:var(--text-3);font-size:13px">Empty folder</div>
          {/if}
        {/if}
      </div>

      <!-- STATUSBAR -->
      <div class="statusbar">
        <div class="path">
          {#if currentShare}
            <span>{shareInfo?.displayName || currentShare}</span>
            {#each pathParts as part, i}
              <span class="psep"> / </span>
              {#if i === pathParts.length - 1}<strong>{part}</strong>{:else}<span>{part}</span>{/if}
            {/each}
          {:else}
            <span>NimOS Storage</span>
          {/if}
        </div>
        {#if selected.size > 0}
          <div class="transfers"><span style="color:var(--accent);font-weight:500">{selected.size} selected</span></div>
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  /* ═══════════════════════════════════════
     EXACT COPY of mockup CSS tokens + styles
     ═══════════════════════════════════════ */
  .files-root {
    width:100%; height:100%;
    display:flex; flex-direction:row; align-items:stretch;
    overflow:hidden;
    background: var(--bg-frame, #111028);
    font-family:'DM Sans',-apple-system,sans-serif;
    color: var(--text-1, rgba(255,255,255,0.92));
  }

  /* ── SIDEBAR ── */
  .sidebar {
    width:190px; flex-shrink:0;
    display:flex; flex-direction:column; gap:2px;
    padding:12px 8px;
    overflow-y:auto;
    background: var(--bg-sidebar, #111028);
  }
  .sidebar::-webkit-scrollbar { width:3px; }
  .sidebar::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.2); border-radius:2px; }

  .sb-header { display:flex; align-items:center; gap:8px; padding:32px 8px 12px; }
  .sb-header .title { font-size:15px; font-weight:600; color:var(--text-1, rgba(255,255,255,0.92)); }
  .sb-section {
    font-size:9px; font-weight:700; letter-spacing:.1em;
    text-transform:uppercase; color:var(--text-3, rgba(255,255,255,0.27));
    padding:10px 8px 3px;
  }
  .sb-item {
    display:flex; align-items:center; gap:8px;
    padding:6px 10px; border-radius:8px; cursor:pointer;
    font-size:12px; color:var(--text-2, rgba(255,255,255,0.50));
    border:1px solid transparent; transition:all .15s;
  }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg, rgba(124,111,255,0.18)); color:var(--text-1); border-color:var(--border-hi, rgba(124,111,255,0.50)); }
  .sb-item .ico { font-size:13px; width:16px; text-align:center; flex-shrink:0; }
  .sb-item.search { border:1px solid var(--border, rgba(255,255,255,0.07)); }
  .sb-storage {
    margin-top:auto; padding:9px 10px;
    background:var(--storage-bg, rgba(255,255,255,0.05));
    border:1px solid var(--border, rgba(255,255,255,0.07)); border-radius:9px;
  }
  .ss-labels { display:flex; justify-content:space-between; font-size:10px; color:var(--text-2); margin-bottom:6px; }
  .ss-labels strong { color:var(--text-1); font-weight:500; }
  .ss-bar { height:3px; background:rgba(128,128,128,0.15); border-radius:2px; overflow:hidden; }
  .ss-fill { height:100%; background:linear-gradient(90deg,var(--accent, #7c6fff),var(--accent2, #c054f0)); }

  /* ── INNER WRAP ── */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner {
    flex:1; border-radius:10px;
    border:1px solid var(--border, rgba(255,255,255,0.07));
    background: var(--bg-inner, #1c1b3a);
    display:flex; flex-direction:column;
    overflow:hidden;
  }

  /* ── TITLEBAR ── */
  .inner-titlebar {
    display:flex; align-items:center; gap:8px;
    padding:10px 14px 9px;
    background: var(--bg-bar, #1c1b3a);
    flex-shrink:0;
  }
  .nav-btn {
    background:none; border:none; cursor:pointer;
    color:var(--text-2); font-size:15px; padding:2px 5px;
    border-radius:6px; line-height:1; transition:all .15s;
    font-family:inherit;
  }
  .nav-btn:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .inner-title { font-size:12px; font-weight:600; color:var(--text-1); }
  .inner-title small { color:var(--text-3); font-weight:400; font-size:11px; margin-left:4px; }
  .tb-right { margin-left:auto; display:flex; align-items:center; gap:6px; }
  .icon-btn {
    width:27px; height:27px;
    background:var(--ibtn-bg, rgba(255,255,255,0.06)); border:1px solid var(--border);
    border-radius:6px; display:flex; align-items:center; justify-content:center;
    cursor:pointer; color:var(--text-2); font-size:12px; transition:all .15s;
  }
  .icon-btn:hover { background:rgba(124,111,255,0.15); color:var(--text-1); }
  .btn-import {
    display:flex; align-items:center; gap:5px; padding:4px 11px;
    background:linear-gradient(135deg, var(--accent, #7c6fff), var(--accent2, #c054f0));
    border:none; border-radius:6px; color:#fff;
    font-family:'DM Sans',sans-serif; font-size:11px; font-weight:600;
    cursor:pointer; box-shadow:0 2px 10px rgba(124,111,255,0.35);
    transition:opacity .15s, transform .1s;
  }
  .btn-import:hover { opacity:.88; transform:translateY(-1px); }

  /* ── FILE GRID ── */
  .file-grid {
    flex:1; overflow-y:auto; padding:14px 12px;
    display:grid; grid-template-columns:repeat(auto-fill, minmax(90px,1fr));
    gap:3px; align-content:start;
  }
  .file-grid::-webkit-scrollbar { width:3px; }
  .file-grid::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .f-item {
    display:flex; flex-direction:column; align-items:center; gap:6px;
    padding:11px 6px 8px; border-radius:9px; cursor:pointer;
    border:1px solid transparent; transition:all .15s;
    animation:fadeUp .35s ease both;
  }
  .f-item:hover { background:rgba(128,128,128,0.08); border-color:var(--border); }
  .f-item.sel { background:var(--active-bg, rgba(124,111,255,0.18)); border-color:var(--border-hi, rgba(124,111,255,0.50)); }
  @keyframes fadeUp {
    from { opacity:0; transform:translateY(7px); }
    to   { opacity:1; transform:translateY(0); }
  }
  .f-item:nth-child(1){animation-delay:.03s}.f-item:nth-child(2){animation-delay:.06s}
  .f-item:nth-child(3){animation-delay:.09s}.f-item:nth-child(4){animation-delay:.12s}
  .f-item:nth-child(5){animation-delay:.15s}.f-item:nth-child(6){animation-delay:.18s}
  .f-item:nth-child(7){animation-delay:.21s}.f-item:nth-child(8){animation-delay:.24s}
  .f-item:nth-child(9){animation-delay:.27s}.f-item:nth-child(10){animation-delay:.30s}
  .f-item:nth-child(11){animation-delay:.33s}.f-item:nth-child(12){animation-delay:.36s}
  .f-item:nth-child(13){animation-delay:.39s}.f-item:nth-child(14){animation-delay:.42s}
  .f-icon { font-size:38px; line-height:1; filter:drop-shadow(0 2px 6px rgba(0,0,0,0.4)); transition:transform .15s; }
  .f-item:hover .f-icon { transform:scale(1.07) translateY(-2px); }
  .f-name { font-size:10px; color:var(--text-1); text-align:center; line-height:1.3; max-width:80px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .f-date { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }

  /* ── STATUSBAR ── */
  .statusbar {
    display:flex; align-items:center; gap:8px;
    padding:9px 14px;
    border-top:1px solid var(--border);
    background: var(--bg-bar, #1c1b3a);
    flex-shrink:0;
    font-size:10px; color:var(--text-3);
    border-radius:0 0 10px 10px;
  }
  .path { display:flex; align-items:center; gap:4px; font-family:'DM Mono',monospace; }
  .path span { color:var(--text-2); }
  .path strong { color:var(--text-1); font-weight:500; }
  .psep { color:var(--text-3); }
  .transfers { margin-left:auto; }
</style>
