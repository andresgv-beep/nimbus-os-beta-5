<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });
  const token = getToken();

  let shares = [];
  let currentShare = '';
  let currentPath = '/';
  let files = [];
  let loading = false;

  let playerEl;
  let isVideo = false;
  let playing = false;
  let currentFile = null;
  let currentSrc = '';
  let duration = 0;
  let currentTime = 0;
  let volume = 0.8;
  let muted = false;

  let playlist = [];
  let playlistIdx = -1;

  let controlsVisible = true;
  let hideTimer = null;

  const AUDIO_EXT = ['mp3','wav','flac','aac','m4a','ogg','opus','wma'];
  const VIDEO_EXT = ['mp4','webm','mkv','avi','mov','ogv'];
  const MEDIA_EXT = [...AUDIO_EXT, ...VIDEO_EXT];

  function getExt(name) { const d = name.lastIndexOf('.'); return d >= 0 ? name.slice(d+1).toLowerCase() : ''; }
  function isMedia(name)     { return MEDIA_EXT.includes(getExt(name)); }
  function isVideoFile(name) { return VIDEO_EXT.includes(getExt(name)); }
  function streamUrl(share, path) {
    return `/api/files/download?share=${encodeURIComponent(share)}&path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
  }

  async function loadShares() {
    try {
      const res = await fetch('/api/files', { headers: hdrs() });
      const data = await res.json();
      shares = data.shares || [];
      if (shares.length > 0 && !currentShare) { currentShare = shares[0].name; loadFiles(); }
    } catch {}
  }

  async function loadFiles() {
    if (!currentShare) return;
    loading = true;
    try {
      const res = await fetch(`/api/files?share=${encodeURIComponent(currentShare)}&path=${encodeURIComponent(currentPath)}`, { headers: hdrs() });
      const data = await res.json();
      files = data.files || [];
    } catch {}
    loading = false;
  }

  function enterFolder(name) {
    currentPath = currentPath === '/' ? '/' + name : currentPath + '/' + name;
    loadFiles();
  }

  function goUp() {
    if (currentPath === '/') return;
    const parts = currentPath.split('/').filter(Boolean); parts.pop();
    currentPath = parts.length ? '/' + parts.join('/') : '/';
    loadFiles();
  }

  function selectShare(name) { currentShare = name; currentPath = '/'; loadFiles(); }

  function playFile(file) {
    const path = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name;
    currentFile = file;
    currentSrc = streamUrl(currentShare, path);
    isVideo = isVideoFile(file.name);
    playing = true;
    playlist = files.filter(f => isMedia(f.name));
    playlistIdx = playlist.findIndex(f => f.name === file.name);
    if (playerEl) { playerEl.src = currentSrc; playerEl.load(); playerEl.play().catch(() => {}); }
    scheduleHide();
  }

  function playNext() {
    if (!playlist.length) return;
    playlistIdx = (playlistIdx + 1) % playlist.length;
    playFile(playlist[playlistIdx]);
  }

  function playPrev() {
    if (!playlist.length) return;
    if (currentTime > 3) { if (playerEl) playerEl.currentTime = 0; return; }
    playlistIdx = (playlistIdx - 1 + playlist.length) % playlist.length;
    playFile(playlist[playlistIdx]);
  }

  function togglePlay() {
    if (!playerEl) return;
    if (playerEl.paused) { playerEl.play().catch(() => {}); scheduleHide(); }
    else { playerEl.pause(); showControls(); }
  }

  function seek(e) {
    if (!playerEl || !duration) return;
    const rect = e.currentTarget.getBoundingClientRect();
    playerEl.currentTime = ((e.clientX - rect.left) / rect.width) * duration;
  }

  function setVol(e) {
    const rect = e.currentTarget.getBoundingClientRect();
    volume = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    if (playerEl) playerEl.volume = volume;
    muted = volume === 0;
  }

  function toggleMute() { muted = !muted; if (playerEl) playerEl.muted = muted; }

  function toggleFullscreen() {
    const el = document.querySelector('.mp-main');
    if (!el) return;
    if (!document.fullscreenElement) el.requestFullscreen().catch(() => {});
    else document.exitFullscreen();
  }

  function showControls() { clearTimeout(hideTimer); controlsVisible = true; }

  function scheduleHide() {
    clearTimeout(hideTimer);
    controlsVisible = true;
    hideTimer = setTimeout(() => { if (playing) controlsVisible = false; }, 3000);
  }

  function onMouseMove() { if (playing) scheduleHide(); else showControls(); }

  function fmtTime(s) {
    if (!s || isNaN(s)) return '0:00';
    return `${Math.floor(s/60)}:${Math.floor(s%60).toString().padStart(2,'0')}`;
  }

  function fmtSize(b) {
    if (!b) return '';
    if (b >= 1e9) return (b/1e9).toFixed(1)+' GB';
    if (b >= 1e6) return (b/1e6).toFixed(1)+' MB';
    return (b/1e3).toFixed(0)+' KB';
  }

  $: breadcrumbs = currentPath === '/' ? [] : currentPath.split('/').filter(Boolean);

  onMount(loadShares);
  onDestroy(() => clearTimeout(hideTimer));
</script>

<div class="mp-root">

  <!-- ── SIDEBAR ── -->
  <div class="mp-sidebar">
    <div class="mp-sidebar-header">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg>
      <span class="mp-title">Media</span>
    </div>

    <div class="mp-section-header">
      <span class="mp-section-label">Cola</span>
      <button class="mp-add-btn" title="Añadir a la lista">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
      </button>
    </div>

    <div class="mp-playlist">
      {#each playlist as item, i}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="mp-pl-item" class:active={i === playlistIdx} on:click={() => { playlistIdx = i; playFile(item); }}>
          {#if isVideoFile(item.name)}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="2" y="2" width="20" height="20" rx="2"/><polygon points="10 8 16 12 10 16 10 8"/></svg>
          {:else}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
          {/if}
          <span class="mp-pl-name">{item.name}</span>
          {#if i === playlistIdx}
            <div class="mp-pl-playing"><span></span><span></span><span></span></div>
          {/if}
        </div>
      {/each}
      {#if playlist.length === 0}
        <div class="mp-pl-empty">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
          <span>Sin archivos en cola</span>
        </div>
      {/if}
    </div>

    <div class="mp-sb-browser">
      <div class="mp-section-header" style="margin-top:10px">
        <span class="mp-section-label">Explorar</span>
      </div>
      {#each shares as s}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="mp-share" class:active={currentShare === s.name} on:click={() => selectShare(s.name)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
          {s.displayName || s.name}
        </div>
      {/each}
    </div>
  </div>

  <!-- ── MAIN ── -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="mp-main" on:mousemove={onMouseMove}>

    {#if currentFile && isVideo && currentSrc}
      <div class="mp-video-wrap">
        <!-- svelte-ignore a11y_media_has_caption -->
        <video
          bind:this={playerEl}
          src={currentSrc}
          bind:duration bind:currentTime bind:volume bind:muted
          on:ended={playNext}
          on:play={() => playing = true}
          on:pause={() => playing = false}
          class="mp-video"
        ></video>
      </div>
    {:else}
      <div class="mp-browser">
        <div class="mp-breadcrumb">
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span class="mp-bc-root" on:click={() => { currentPath='/'; loadFiles(); }}>{currentShare || '—'}</span>
          {#each breadcrumbs as crumb, i}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:10px;height:10px;color:var(--text-3);flex-shrink:0"><polyline points="9 18 15 12 9 6"/></svg>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <span class="mp-bc-crumb" on:click={() => { currentPath='/'+breadcrumbs.slice(0,i+1).join('/'); loadFiles(); }}>{crumb}</span>
          {/each}
        </div>
        {#if loading}
          <div class="mp-loading"><div class="spinner"></div></div>
        {:else}
          <div class="mp-file-list">
            {#if currentPath !== '/'}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="mp-file is-dir" on:click={goUp}>
                <div class="mp-file-ico dir"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="15 18 9 12 15 6"/></svg></div>
                <span class="mp-file-name" style="color:var(--text-3)">Volver</span>
              </div>
            {/if}
            {#each files as file}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="mp-file"
                class:is-media={isMedia(file.name)}
                class:is-playing={currentFile?.name === file.name}
                class:is-dir={file.isDirectory}
                on:click={() => file.isDirectory ? enterFolder(file.name) : isMedia(file.name) ? playFile(file) : null}>
                <div class="mp-file-ico"
                  class:dir={file.isDirectory}
                  class:video={!file.isDirectory && isVideoFile(file.name)}
                  class:audio={!file.isDirectory && !isVideoFile(file.name) && isMedia(file.name)}>
                  {#if file.isDirectory}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                  {:else if isVideoFile(file.name)}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="2" y="2" width="20" height="20" rx="2"/><polygon points="10 8 16 12 10 16 10 8"/></svg>
                  {:else if isMedia(file.name)}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
                  {:else}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                  {/if}
                </div>
                <span class="mp-file-name">{file.name}</span>
                <span class="mp-file-size">{file.isDirectory ? '' : fmtSize(file.size)}</span>
              </div>
            {/each}
            {#if files.length === 0}<div class="mp-empty">Carpeta vacía</div>{/if}
          </div>
        {/if}
      </div>
      {#if currentSrc && !isVideo}
        <!-- svelte-ignore a11y_media_has_caption -->
        <audio bind:this={playerEl} src={currentSrc}
          bind:duration bind:currentTime bind:volume bind:muted
          on:ended={playNext}
          on:play={() => playing = true}
          on:pause={() => playing = false}></audio>
      {/if}
    {/if}

    <!-- Controles flotantes -->
    {#if currentFile}
      <div class="mp-controls" class:hidden={!controlsVisible}>
        <div class="mp-progress-row">
          <span class="mp-time">{fmtTime(currentTime)}</span>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="mp-progress" on:click={seek}>
            <div class="mp-progress-fill" style="width:{duration ? (currentTime/duration)*100 : 0}%">
              <div class="mp-progress-thumb"></div>
            </div>
          </div>
          <span class="mp-time">{fmtTime(duration)}</span>
        </div>
        <div class="mp-btns-row">
          <div class="mp-now">
            <div class="mp-now-art" class:video={isVideo}>
              {#if isVideo}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><rect x="2" y="2" width="20" height="20" rx="2"/><polygon points="10 8 16 12 10 16 10 8"/></svg>
              {:else}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
              {/if}
            </div>
            <div class="mp-now-info">
              <div class="mp-now-name">{currentFile.name}</div>
              <div class="mp-now-path">{currentShare}{currentPath === '/' ? '' : currentPath}</div>
            </div>
          </div>
          <div class="mp-transport">
            <button class="mp-btn" on:click={playPrev}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="19 20 9 12 19 4 19 20"/><line x1="5" y1="19" x2="5" y2="5"/></svg>
            </button>
            <button class="mp-btn play" on:click={togglePlay}>
              {#if playing}
                <svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/></svg>
              {:else}
                <svg viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg>
              {/if}
            </button>
            <button class="mp-btn" on:click={playNext}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 4 15 12 5 20 5 4"/><line x1="19" y1="5" x2="19" y2="19"/></svg>
            </button>
          </div>
          <div class="mp-right">
            <div class="mp-vol-wrap">
              <button class="mp-btn small" on:click={toggleMute}>
                {#if muted || volume === 0}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/></svg>
                {:else if volume < 0.5}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/></svg>
                {:else}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M15.54 8.46a5 5 0 0 1 0 7.07"/></svg>
                {/if}
              </button>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="mp-vol-track" on:click={setVol}>
                <div class="mp-vol-fill" style="width:{muted ? 0 : volume*100}%"></div>
              </div>
            </div>
            <button class="mp-btn fullscreen" on:click={toggleFullscreen} title="Pantalla completa">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/></svg>
            </button>
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .mp-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'Inter',-apple-system,sans-serif; color:var(--text-1); }

  /* Sidebar */
  .mp-sidebar { width:200px; flex-shrink:0; padding:12px 8px; background:var(--bg-sidebar); border-right:1px solid var(--border); display:flex; flex-direction:column; overflow:hidden; }
  .mp-sidebar-header { display:flex; align-items:center; gap:8px; padding:16px 10px 14px; color:var(--text-1); }
  .mp-title { font-size:15px; font-weight:600; }
  .mp-section-header { display:flex; align-items:center; justify-content:space-between; padding:4px 8px; }
  .mp-section-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; }
  .mp-add-btn { width:18px; height:18px; border-radius:4px; border:1px solid var(--border); background:transparent; color:var(--text-3); cursor:pointer; display:flex; align-items:center; justify-content:center; transition:all .15s; }
  .mp-add-btn svg { width:10px; height:10px; }
  .mp-add-btn:hover { color:var(--text-1); border-color:var(--border-hi); background:var(--ibtn-bg); }
  .mp-playlist { display:flex; flex-direction:column; gap:1px; overflow-y:auto; flex:1; margin-top:2px; }
  .mp-playlist::-webkit-scrollbar { width:3px; }
  .mp-playlist::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .mp-pl-item { display:flex; align-items:center; gap:7px; padding:6px 8px; border-radius:6px; font-size:11px; color:var(--text-3); cursor:pointer; transition:all .1s; }
  .mp-pl-item svg { width:11px; height:11px; flex-shrink:0; opacity:.6; }
  .mp-pl-item:hover { background:rgba(128,128,128,0.06); color:var(--text-2); }
  .mp-pl-item.active { color:var(--accent); background:var(--active-bg); }
  .mp-pl-item.active svg { opacity:1; }
  .mp-pl-name { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .mp-pl-playing { display:flex; align-items:flex-end; gap:2px; height:12px; flex-shrink:0; }
  .mp-pl-playing span { width:2px; border-radius:1px; background:var(--accent); animation:bar-bounce .8s ease-in-out infinite; }
  .mp-pl-playing span:nth-child(1) { height:5px; animation-delay:0s; }
  .mp-pl-playing span:nth-child(2) { height:9px; animation-delay:.2s; }
  .mp-pl-playing span:nth-child(3) { height:6px; animation-delay:.4s; }
  @keyframes bar-bounce { 0%,100%{transform:scaleY(0.4)} 50%{transform:scaleY(1)} }
  .mp-pl-empty { display:flex; flex-direction:column; align-items:center; gap:8px; padding:24px 8px; opacity:.4; }
  .mp-pl-empty svg { width:22px; height:22px; }
  .mp-pl-empty span { font-size:10px; color:var(--text-3); text-align:center; }
  .mp-sb-browser { display:flex; flex-direction:column; border-top:1px solid var(--border); padding-top:4px; }
  .mp-share { display:flex; align-items:center; gap:7px; padding:6px 8px; border-radius:7px; font-size:12px; color:var(--text-2); cursor:pointer; transition:all .12s; }
  .mp-share svg { width:13px; height:13px; flex-shrink:0; opacity:.6; }
  .mp-share:hover { background:rgba(128,128,128,0.08); color:var(--text-1); }
  .mp-share.active { background:var(--active-bg); color:var(--text-1); }

  /* Main */
  .mp-main { flex:1; position:relative; overflow:hidden; background:#0d0c1e; }
  .mp-video-wrap { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; background:#000; }
  .mp-video { width:100%; height:100%; object-fit:contain; }
  .mp-browser { position:absolute; inset:0; overflow-y:auto; padding:16px 20px 80px; background:var(--bg-inner); }
  .mp-browser::-webkit-scrollbar { width:3px; }
  .mp-browser::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .mp-breadcrumb { display:flex; align-items:center; gap:4px; margin-bottom:12px; font-size:11px; }
  .mp-bc-root { color:var(--text-2); cursor:pointer; font-weight:500; transition:color .1s; }
  .mp-bc-root:hover { color:var(--text-1); }
  .mp-bc-crumb { color:var(--text-3); cursor:pointer; font-family:'DM Mono',monospace; font-size:10px; transition:color .1s; }
  .mp-bc-crumb:hover { color:var(--text-2); }
  .mp-file-list { display:flex; flex-direction:column; gap:1px; }
  .mp-file { display:flex; align-items:center; gap:10px; padding:7px 8px; border-radius:7px; font-size:12px; color:var(--text-2); transition:all .1s; }
  .mp-file.is-dir { cursor:pointer; } .mp-file.is-dir:hover { background:rgba(128,128,128,0.06); color:var(--text-1); }
  .mp-file.is-media { cursor:pointer; } .mp-file.is-media:hover { background:var(--active-bg); color:var(--text-1); }
  .mp-file.is-playing { background:var(--active-bg); color:var(--accent); }
  .mp-file-ico { width:28px; height:28px; border-radius:6px; flex-shrink:0; display:flex; align-items:center; justify-content:center; background:rgba(255,255,255,0.05); }
  .mp-file-ico svg { width:14px; height:14px; color:var(--text-3); }
  .mp-file-ico.dir   { background:rgba(251,191,36,0.10); } .mp-file-ico.dir svg   { color:var(--amber); }
  .mp-file-ico.video { background:rgba(124,111,255,0.10); } .mp-file-ico.video svg { color:var(--accent); }
  .mp-file-ico.audio { background:rgba(74,222,128,0.10);  } .mp-file-ico.audio svg { color:var(--green); }
  .mp-file-name { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .mp-file-size { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex-shrink:0; }
  .mp-empty { font-size:11px; color:var(--text-3); padding:24px 0; text-align:center; }
  .mp-loading { display:flex; justify-content:center; padding:30px; }
  .spinner { width:20px; height:20px; border-radius:50%; border:2px solid rgba(255,255,255,0.08); border-top-color:var(--accent); animation:spin .7s linear infinite; }
  @keyframes spin { to{transform:rotate(360deg)} }

  /* Controles flotantes */
  .mp-controls { position:absolute; bottom:0; left:0; right:0; padding:20px 20px 16px; background:linear-gradient(to top,rgba(0,0,0,0.88) 0%,rgba(0,0,0,0.45) 60%,transparent 100%); display:flex; flex-direction:column; gap:10px; transition:opacity .35s ease; z-index:10; }
  .mp-controls.hidden { opacity:0; pointer-events:none; }
  .mp-progress-row { display:flex; align-items:center; gap:8px; }
  .mp-time { font-size:10px; color:rgba(255,255,255,0.55); font-family:'DM Mono',monospace; min-width:32px; text-align:center; }
  .mp-progress { flex:1; height:3px; border-radius:2px; background:rgba(255,255,255,0.15); cursor:pointer; position:relative; transition:height .15s; }
  .mp-progress:hover { height:5px; }
  .mp-progress-fill { height:100%; border-radius:2px; background:linear-gradient(90deg,var(--accent),var(--accent2)); position:relative; }
  .mp-progress-thumb { position:absolute; right:-5px; top:50%; transform:translateY(-50%) scale(0); width:11px; height:11px; border-radius:50%; background:#fff; transition:transform .15s; }
  .mp-progress:hover .mp-progress-thumb { transform:translateY(-50%) scale(1); }
  .mp-btns-row { display:flex; align-items:center; gap:4px; }
  .mp-now { display:flex; align-items:center; gap:9px; flex:1; min-width:0; }
  .mp-now-art { width:34px; height:34px; border-radius:7px; flex-shrink:0; background:rgba(124,111,255,0.20); border:1px solid rgba(124,111,255,0.30); display:flex; align-items:center; justify-content:center; }
  .mp-now-art svg { width:15px; height:15px; color:var(--accent); }
  .mp-now-art.video { background:rgba(96,165,250,0.15); border-color:rgba(96,165,250,0.25); }
  .mp-now-art.video svg { color:var(--blue); }
  .mp-now-info { overflow:hidden; min-width:0; }
  .mp-now-name { font-size:12px; font-weight:600; color:#fff; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .mp-now-path { font-size:9px; color:rgba(255,255,255,0.4); font-family:'DM Mono',monospace; }
  .mp-transport { display:flex; align-items:center; gap:6px; flex:1; justify-content:center; margin-right:80px; }
  .mp-transport .mp-btn svg { width:17px; height:17px; }
  .mp-btn { width:34px; height:34px; border:none; background:none; color:rgba(255,255,255,0.7); cursor:pointer; border-radius:8px; display:flex; align-items:center; justify-content:center; transition:all .12s; }
  .mp-btn svg { width:15px; height:15px; }
  .mp-btn:hover { background:rgba(255,255,255,0.10); color:#fff; }
  .mp-btn.play { width:42px; height:42px; border-radius:50%; background:rgba(255,255,255,0.15); backdrop-filter:blur(8px); border:1px solid rgba(255,255,255,0.25); color:#fff; }
  .mp-btn.play svg { width:18px; height:18px; }
  .mp-btn.play:hover { background:rgba(255,255,255,0.25); }
  .mp-btn.small { width:28px; height:28px; }
  .mp-btn.small svg { width:13px; height:13px; }
  .mp-right { display:flex; align-items:center; gap:4px; flex-shrink:0; }
  .mp-vol-wrap { display:flex; align-items:center; gap:6px; }
  .mp-vol-track { width:68px; height:3px; border-radius:2px; background:rgba(255,255,255,0.15); cursor:pointer; transition:height .15s; }
  .mp-vol-track:hover { height:5px; }
  .mp-vol-fill { height:100%; border-radius:2px; background:rgba(255,255,255,0.7); }
</style>
