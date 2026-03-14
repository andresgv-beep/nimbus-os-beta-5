<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let activeTab = 'disks';

  let loading = true;
  let pools = [];
  let eligible = [];
  let nvme = [];
  let selectedDisk = null;

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  async function load() {
    loading = true;
    try {
      const [statusRes, disksRes] = await Promise.all([
        fetch('/api/storage/status', { headers: hdrs() }),
        fetch('/api/storage/disks',  { headers: hdrs() }),
      ]);
      const status = await statusRes.json();
      const disks  = await disksRes.json();
      pools    = status.pools    || [];
      eligible = disks.eligible  || [];
      nvme     = disks.nvme      || [];
    } catch (e) {
      console.error('[Storage] load failed', e);
    }
    loading = false;
  }

  onMount(load);

  $: totalBytes = [...eligible, ...nvme].reduce((a, d) => a + (d.size || 0), 0);
  $: usedBytes  = pools.reduce((a, p) => a + (p.used || 0), 0);
  $: usedPct    = totalBytes > 0 ? (usedBytes / totalBytes) * 100 : 0;

  function fmt(bytes) {
    if (!bytes) return '—';
    const tb = bytes / 1e12;
    if (tb >= 1) return tb.toFixed(1) + ' TB';
    return (bytes / 1e9).toFixed(1) + ' GB';
  }

  function selectDisk(d) {
    selectedDisk = selectedDisk?.name === d.name ? null : d;
  }

  $: hddSlots  = Array.from({ length: 4 }, (_, i) => eligible[i] || null);
  $: nvmeSlots = Array.from({ length: 2 }, (_, i) => nvme[i]      || null);
</script>

<div class="storage-root">
  <div class="s-body">

    {#if loading}
      <div class="s-loading"><div class="spinner"></div></div>

    {:else if activeTab === 'disks'}

      <!-- HDD section -->
      <div class="disk-section">
        <div class="disk-section-label">Discos · HDD / SSD</div>
        <div class="disk-slots-wrap">
          {#each hddSlots as disk, i}
            {#if disk}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="disk-slot" class:selected={selectedDisk?.name === disk.name} on:click={() => selectDisk(disk)}>
                <svg width="58" height="130" viewBox="0 0 58 130" fill="none">
                  <defs>
                    <linearGradient id="hdd{i}bg" x1="0" y1="0" x2="0" y2="130" gradientUnits="userSpaceOnUse">
                      <stop offset="0%" stop-color="#8b7fff"/>
                      <stop offset="100%" stop-color="#5a48dd"/>
                    </linearGradient>
                  </defs>
                  <rect x="0" y="0" width="58" height="130" rx="10" fill="url(#hdd{i}bg)"/>
                  <rect x="4" y="4" width="50" height="104" rx="7" fill="rgba(0,0,0,0.45)"/>
                  <text x="11" y="20" font-size="12" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.85)">{i+1}</text>
                  <text x="29" y="100" text-anchor="middle" font-size="9" font-weight="600" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.55)">{fmt(disk.size)}</text>
                  <circle cx="29" cy="120" r="4" fill="#4ade80" style="animation:ledBlink 2s ease-in-out {i*0.5}s infinite"/>
                  <circle cx="29" cy="120" r="7" fill="rgba(74,222,128,0.18)" style="animation:ledBlink 2s ease-in-out {i*0.5}s infinite"/>
                </svg>
                <div class="disk-label">{disk.name}</div>
              </div>
            {:else}
              <div class="disk-slot empty">
                <svg width="58" height="130" viewBox="0 0 58 130" fill="none">
                  <rect x="0" y="0" width="58" height="130" rx="10" fill="rgba(128,128,128,0.14)"/>
                  <rect x="0.75" y="0.75" width="56.5" height="128.5" rx="9.5" stroke="rgba(255,255,255,0.12)" stroke-width="1.5" fill="none"/>
                  <rect x="4" y="4" width="50" height="104" rx="7" fill="rgba(0,0,0,0.20)"/>
                  <text x="11" y="20" font-size="12" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.35)">{i+1}</text>
                  <circle cx="29" cy="120" r="4" fill="rgba(255,255,255,0.12)"/>
                </svg>
                <div class="disk-label empty-label">vacío</div>
              </div>
            {/if}
          {/each}

          <!-- Info panel -->
          <div class="disk-info-panel">
            {#if selectedDisk}
              <div class="di-name">{selectedDisk.model || selectedDisk.name}</div>
              <div class="di-serial">{selectedDisk.serial || '—'}</div>
              <div class="di-row"><span>Dispositivo</span><span>{selectedDisk.name}</span></div>
              <div class="di-row"><span>Capacidad</span><span>{fmt(selectedDisk.size)}</span></div>
              <div class="di-row"><span>Tipo</span><span>{selectedDisk.rota ? 'HDD' : 'SSD'}</span></div>
              {#if selectedDisk.transport}
                <div class="di-row"><span>Interfaz</span><span>{selectedDisk.transport.toUpperCase()}</span></div>
              {/if}
              <div class="di-tags">
                {#if selectedDisk.provisioned}
                  <span class="di-tag green">En pool</span>
                {:else}
                  <span class="di-tag">Libre</span>
                {/if}
              </div>
            {:else}
              <div class="di-empty">
                <div class="di-empty-icon">⊙</div>
                <div>Selecciona un disco</div>
              </div>
            {/if}
          </div>
        </div>
      </div>

      <!-- NVMe section -->
      <div class="disk-section" style="margin-top:18px">
        <div class="disk-section-label">NVMe · M.2</div>
        <div class="nvme-slots-wrap">
          {#each nvmeSlots as disk, i}
            {#if disk}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="disk-slot" class:selected={selectedDisk?.name === disk.name} on:click={() => selectDisk(disk)}>
                <svg width="42" height="130" viewBox="0 0 42 130" fill="none">
                  <defs>
                    <linearGradient id="nv{i}bg" x1="0" y1="0" x2="0" y2="130" gradientUnits="userSpaceOnUse">
                      <stop offset="0%" stop-color="#2e2a4a"/>
                      <stop offset="100%" stop-color="#1e1a36"/>
                    </linearGradient>
                    <linearGradient id="nv{i}slot" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stop-color="rgba(124,111,255,0.22)"/>
                      <stop offset="100%" stop-color="rgba(124,111,255,0.08)"/>
                    </linearGradient>
                  </defs>
                  <rect x="0" y="0" width="42" height="130" rx="8" fill="url(#nv{i}bg)"/>
                  <rect x="0.75" y="0.75" width="40.5" height="128.5" rx="7.5" stroke="rgba(124,111,255,0.35)" stroke-width="1.5" fill="none"/>
                  <rect x="10" y="3" width="22" height="5" rx="1.5" fill="rgba(255,255,255,0.10)"/>
                  <rect x="13" y="3" width="3" height="3" rx="0.5" fill="rgba(255,255,255,0.22)"/>
                  <rect x="18" y="3" width="3" height="3" rx="0.5" fill="rgba(255,255,255,0.22)"/>
                  <rect x="23" y="3" width="3" height="3" rx="0.5" fill="rgba(255,255,255,0.22)"/>
                  <text x="21" y="22" text-anchor="middle" font-size="10" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.75)">{String.fromCharCode(65+i)}</text>
                  <rect x="6" y="28" width="30" height="18" rx="3" fill="url(#nv{i}slot)" stroke="rgba(124,111,255,0.25)" stroke-width="0.75"/>
                  <rect x="6" y="52" width="30" height="18" rx="3" fill="url(#nv{i}slot)" stroke="rgba(124,111,255,0.25)" stroke-width="0.75"/>
                  <rect x="6" y="76" width="30" height="18" rx="3" fill="url(#nv{i}slot)" stroke="rgba(124,111,255,0.25)" stroke-width="0.75"/>
                  <text x="21" y="112" text-anchor="middle" font-size="8" font-weight="600" font-family="DM Mono,monospace" fill="rgba(255,255,255,0.45)">{fmt(disk.size)}</text>
                  <rect x="8" y="119" width="26" height="4" rx="2" fill="rgba(74,222,128,0.12)"/>
                  <rect x="8" y="119" width="26" height="4" rx="2" fill="#4ade80" style="animation:ledBlink 2.2s ease-in-out {i*0.6}s infinite"/>
                </svg>
                <div class="disk-label">{disk.name}</div>
              </div>
            {:else}
              <div class="disk-slot empty">
                <svg width="42" height="130" viewBox="0 0 42 130" fill="none">
                  <rect x="0" y="0" width="42" height="130" rx="8" fill="rgba(128,128,128,0.12)"/>
                  <rect x="0.75" y="0.75" width="40.5" height="128.5" rx="7.5" stroke="rgba(255,255,255,0.12)" stroke-width="1.5" stroke-dasharray="5 4" fill="none"/>
                  <rect x="10" y="3" width="22" height="5" rx="1.5" fill="rgba(255,255,255,0.08)"/>
                  <text x="21" y="22" text-anchor="middle" font-size="10" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.30)">{String.fromCharCode(65+i)}</text>
                  <rect x="6" y="28" width="30" height="18" rx="3" fill="rgba(255,255,255,0.05)" stroke="rgba(255,255,255,0.10)" stroke-width="0.75"/>
                  <rect x="6" y="52" width="30" height="18" rx="3" fill="rgba(255,255,255,0.05)" stroke="rgba(255,255,255,0.10)" stroke-width="0.75"/>
                  <rect x="6" y="76" width="30" height="18" rx="3" fill="rgba(255,255,255,0.05)" stroke="rgba(255,255,255,0.10)" stroke-width="0.75"/>
                  <rect x="8" y="119" width="26" height="4" rx="2" fill="rgba(255,255,255,0.08)"/>
                </svg>
                <div class="disk-label empty-label">vacío</div>
              </div>
            {/if}
          {/each}
        </div>
      </div>

      <!-- Storage bar -->
      <div class="storage-bar-section">
        <div class="sbs-meta">
          <span class="sbs-label">Capacidad total · {eligible.length + nvme.length} discos</span>
          <span class="sbs-value">{fmt(usedBytes)} / {fmt(totalBytes)} · {usedPct.toFixed(0)}%</span>
        </div>
        <div class="sbs-track">
          <div class="sbs-fill" style="width:{Math.max(0.5, usedPct)}%"></div>
        </div>
      </div>

      <!-- Legend -->
      <div class="disk-legend">
        <div class="dl-item"><div class="dl-dot" style="background:var(--green)"></div>Sano</div>
        <div class="dl-item"><div class="dl-dot" style="background:var(--amber)"></div>Degradado</div>
        <div class="dl-item"><div class="dl-dot" style="background:var(--red)"></div>Error</div>
        <div class="dl-item"><div class="dl-dot" style="background:rgba(128,128,128,0.3)"></div>Vacío</div>
      </div>

    {:else if activeTab === 'pools'}
      <div class="section-label">Pools de almacenamiento</div>
      {#if pools.length === 0}
        <p class="coming-soon">No hay pools configurados</p>
      {:else}
        {#each pools as pool}
          <div class="pool-row">
            <div class="pool-led" class:healthy={pool.health === 'ONLINE'}></div>
            <div class="pool-info">
              <div class="pool-name">{pool.name} {#if pool.primary}<span class="pool-primary">Primary</span>{/if}</div>
              <div class="pool-meta">{pool.type || 'RAID'} · {pool.mountpoint || '—'} · {fmt(pool.size)}</div>
            </div>
            <div class="pool-badge" class:green={pool.health === 'ONLINE'}>{pool.health || '—'}</div>
          </div>
        {/each}
      {/if}

    {:else if activeTab === 'health'}
      <div class="section-label">Estado de salud</div>
      <p class="coming-soon">SMART monitoring — coming soon</p>

    {:else if activeTab === 'restore'}
      <div class="section-label">Restaurar pool</div>
      <p class="coming-soon">Pool restore — coming soon</p>
    {/if}

  </div>
</div>

<style>
  .storage-root { width:100%; height:100%; display:flex; flex-direction:column; overflow:hidden; }
  .s-body { flex:1; overflow-y:auto; padding:18px 20px; }
  .s-body::-webkit-scrollbar { width:3px; }
  .s-body::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .s-loading { display:flex; align-items:center; justify-content:center; height:100%; }
  .spinner {
    width:28px; height:28px; border-radius:50%;
    border:2.5px solid rgba(255,255,255,0.1);
    border-top-color:var(--accent);
    animation:spin .7s linear infinite;
  }
  @keyframes spin { to { transform:rotate(360deg); } }

  /* ── DISK SLOTS ── */
  .disk-section { }
  .disk-section-label {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.08em; margin-bottom:10px;
  }
  .disk-slots-wrap { display:flex; gap:8px; align-items:flex-start; }
  .nvme-slots-wrap  { display:flex; gap:8px; align-items:flex-start; }

  .disk-slot { display:flex; flex-direction:column; align-items:center; gap:4px; cursor:pointer; transition:transform .15s; }
  .disk-slot:not(.empty):hover { transform:translateY(-2px); }
  .disk-slot.empty { opacity:.35; cursor:default; pointer-events:none; }
  .disk-slot.selected { transform:translateY(-2px); }
  .disk-slot.selected svg { filter:drop-shadow(0 0 6px rgba(124,111,255,0.5)); }

  .disk-label { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; text-align:center; }
  .empty-label { opacity:.5; }

  @keyframes ledBlink { 0%,100%{opacity:.9} 50%{opacity:.2} }

  /* ── DISK INFO PANEL ── */
  .disk-info-panel {
    flex:1; margin-left:4px;
    padding:12px 14px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    display:flex; flex-direction:column; gap:5px;
    justify-content:center; align-self:stretch; min-width:0;
  }
  .di-empty { display:flex; flex-direction:column; align-items:center; gap:6px; color:var(--text-3); font-size:11px; }
  .di-empty-icon { font-size:22px; opacity:.4; }
  .di-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .di-serial { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .di-row {
    display:flex; justify-content:space-between;
    font-size:10px; color:var(--text-2); border-bottom:1px solid var(--border); padding:3px 0;
  }
  .di-row span:last-child { color:var(--text-1); font-family:'DM Mono',monospace; font-size:9px; }
  .di-tags { display:flex; gap:5px; margin-top:3px; }
  .di-tag {
    padding:2px 7px; border-radius:4px; font-size:9px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-2);
    font-family:'DM Mono',monospace;
  }
  .di-tag.green { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.25); color:var(--green); }

  /* ── STORAGE BAR ── */
  .storage-bar-section { margin-top:16px; width:50%; }
  .sbs-meta { display:flex; justify-content:space-between; margin-bottom:5px; }
  .sbs-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .sbs-value { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .sbs-track { height:5px; background:rgba(128,128,128,0.12); border-radius:3px; overflow:hidden; }
  .sbs-fill  { height:100%; border-radius:3px; background:linear-gradient(90deg, var(--accent), var(--accent2)); }

  /* ── LEGEND ── */
  .disk-legend { display:flex; gap:14px; margin-top:12px; }
  .dl-item { display:flex; align-items:center; gap:5px; font-size:10px; color:var(--text-3); }
  .dl-dot  { width:7px; height:7px; border-radius:2px; flex-shrink:0; }

  /* ── POOLS TAB ── */
  .section-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; margin-bottom:12px; }
  .pool-row {
    display:flex; align-items:center; gap:10px;
    padding:10px 12px; border-radius:8px; margin-bottom:6px;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .pool-led { width:7px; height:7px; border-radius:50%; background:rgba(128,128,128,0.3); flex-shrink:0; }
  .pool-led.healthy { background:var(--green); box-shadow:0 0 5px rgba(74,222,128,0.6); }
  .pool-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .pool-primary { font-size:9px; font-weight:400; color:var(--text-3); margin-left:5px; }
  .pool-meta { font-size:10px; color:var(--text-3); margin-top:1px; }
  .pool-badge { margin-left:auto; padding:3px 8px; border-radius:20px; font-size:9px; font-weight:600; background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-2); }
  .pool-badge.green { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.25); color:var(--green); }
  .coming-soon { color:var(--text-3); font-size:12px; }
</style>
