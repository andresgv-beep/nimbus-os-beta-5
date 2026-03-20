<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });
  const token = getToken();

  // ── State ──
  let shares = [];
  let currentShare = '';
  let currentPath = '/';
  let files = [];
  let loading = false;

  // ── Player state ──
  let playerEl;
  let isVideo = false;
  let playing = false;
  let currentFile = null;
  let currentSrc = '';
  let duration = 0;
  let currentTime = 0;
  let volume = 0.8;
  let muted = false;

  // ── Playlist ──
  let playlist = [];
  let playlistIdx = -1;

  const AUDIO_EXT = ['mp3','wav','flac','aac','m4a','ogg','opus','wma'];
  const VIDEO_EXT = ['mp4','webm','mkv','avi','mov','ogv'];
  const MEDIA_EXT = [...AUDIO_EXT, ...VIDEO_EXT];

  function getExt(name) {
    const dot = name.lastIndexOf('.');
    return dot >= 0 ? name.slice(dot + 1).toLowerCase() : '';
  }

  function isMedia(name) { return MEDIA_EXT.includes(getExt(name)); }
  function isVideoFile(name) { return VIDEO_EXT.includes(getExt(name)); }

  function streamUrl(share, path) {
    return `/api/files/download?share=${encodeURIComponent(share)}&path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
  }

  // ── Load shares ──
  async function loadShares() {
    try {
      const res = await fetch('/api/files', { headers: hdrs() });
      const data = await res.json();
      shares = data.shares || [];
      if (shares.length > 0 && !currentShare) {
        currentShare = shares[0].name;
        loadFiles();
      }
    } catch {}
  }

  // ── Load files ──
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
    const parts = currentPath.split('/').filter(Boolean);
    parts.pop();
    currentPath = '/' + parts.join('/');
    if (currentPath === '') currentPath = '/';
    loadFiles();
  }

  function selectShare(name) {
    currentShare = name;
    currentPath = '/';
    loadFiles();
  }

  // ── Play ──
  function playFile(file) {
    const path = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name;
    currentFile = file;
    currentSrc = streamUrl(currentShare, path);
    isVideo = isVideoFile(file.name);
    playing = true;

    // Build playlist from current folder
    playlist = files.filter(f => isMedia(f.name));
    playlistIdx = playlist.findIndex(f => f.name === file.name);

    if (playerEl) {
      playerEl.src = currentSrc;
      playerEl.load();
      playerEl.play().catch(() => {});
    }
  }

  function playNext() {
    if (playlist.length === 0) return;
    playlistIdx = (playlistIdx + 1) % playlist.length;
    playFile(playlist[playlistIdx]);
  }

  function playPrev() {
    if (playlist.length === 0) return;
    if (currentTime > 3) {
      // Restart current track
      if (playerEl) { playerEl.currentTime = 0; }
      return;
    }
    playlistIdx = (playlistIdx - 1 + playlist.length) % playlist.length;
    playFile(playlist[playlistIdx]);
  }

  function togglePlay() {
    if (!playerEl) return;
    if (playerEl.paused) { playerEl.play().catch(() => {}); }
    else { playerEl.pause(); }
  }

  function seek(e) {
    if (!playerEl || !duration) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const pct = (e.clientX - rect.left) / rect.width;
    playerEl.currentTime = pct * duration;
  }

  function setVol(e) {
    const rect = e.currentTarget.getBoundingClientRect();
    volume = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    if (playerEl) playerEl.volume = volume;
    muted = volume === 0;
  }

  function toggleMute() {
    muted = !muted;
    if (playerEl) playerEl.muted = muted;
  }

  function fmtTime(s) {
    if (!s || isNaN(s)) return '0:00';
    const m = Math.floor(s / 60);
    const sec = Math.floor(s % 60);
    return `${m}:${sec.toString().padStart(2, '0')}`;
  }

  function fmtSize(b) {
    if (!b) return '';
    if (b >= 1e9) return (b / 1e9).toFixed(1) + ' GB';
    if (b >= 1e6) return (b / 1e6).toFixed(1) + ' MB';
    return (b / 1e3).toFixed(0) + ' KB';
  }

  onMount(loadShares);
</script>

<div class="mp-root">
  <!-- Sidebar: shares + browser -->
  <div class="mp-sidebar">
    <div class="mp-sidebar-header">
      <span class="mp-title">Media</span>
    </div>

    <!-- Shares -->
    <div class="mp-section-label">Carpetas</div>
    {#each shares as s}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="mp-share" class:active={currentShare === s.name} on:click={() => selectShare(s.name)}>
        📁 {s.displayName || s.name}
      </div>
    {/each}

    <!-- Playlist -->
    {#if playlist.length > 0}
      <div class="mp-section-label" style="margin-top:12px">Cola</div>
      <div class="mp-playlist">
        {#each playlist as item, i}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="mp-pl-item" class:active={i === playlistIdx} on:click={() => { playlistIdx = i; playFile(item); }}>
            <span class="mp-pl-icon">{isVideoFile(item.name) ? '🎬' : '🎵'}</span>
            <span class="mp-pl-name">{item.name}</span>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Main area -->
  <div class="mp-main">
    <!-- File browser -->
    <div class="mp-browser" class:has-player={currentFile}>
      <div class="mp-breadcrumb">
        {#if currentPath !== '/'}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span class="mp-bc-back" on:click={goUp}>‹</span>
        {/if}
        <span class="mp-bc-path">{currentShare}{currentPath}</span>
      </div>

      {#if loading}
        <div class="mp-loading"><div class="spinner"></div></div>
      {:else}
        <div class="mp-file-list">
          {#each files as file}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div
              class="mp-file"
              class:is-media={isMedia(file.name)}
              class:is-playing={currentFile?.name === file.name}
              class:is-dir={file.isDirectory}
              on:click={() => file.isDirectory ? enterFolder(file.name) : isMedia(file.name) ? playFile(file) : null}
            >
              <span class="mp-file-icon">
                {#if file.isDirectory}📁
                {:else if isVideoFile(file.name)}🎬
                {:else if isMedia(file.name)}🎵
                {:else}📄
                {/if}
              </span>
              <span class="mp-file-name">{file.name}</span>
              <span class="mp-file-size">{file.isDirectory ? '' : fmtSize(file.size)}</span>
            </div>
          {/each}
          {#if files.length === 0}
            <div class="mp-empty">Carpeta vacía</div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Video display -->
    {#if isVideo && currentSrc}
      <div class="mp-video-wrap">
        <!-- svelte-ignore a11y_media_has_caption -->
        <video
          bind:this={playerEl}
          src={currentSrc}
          bind:duration
          bind:currentTime
          bind:volume
          bind:muted
          bind:paused={playing}
          on:ended={playNext}
          on:play={() => playing = true}
          on:pause={() => playing = false}
          class="mp-video"
        ></video>
      </div>
    {/if}

    <!-- Audio element (hidden) -->
    {#if !isVideo && currentSrc}
      <!-- svelte-ignore a11y_media_has_caption -->
      <audio
        bind:this={playerEl}
        src={currentSrc}
        bind:duration
        bind:currentTime
        bind:volume
        bind:muted
        on:ended={playNext}
        on:play={() => playing = true}
        on:pause={() => playing = false}
      ></audio>
    {/if}

    <!-- Player controls -->
    {#if currentFile}
      <div class="mp-controls">
        <!-- Now playing -->
        <div class="mp-now">
          <span class="mp-now-icon">{isVideo ? '🎬' : '🎵'}</span>
          <div class="mp-now-info">
            <div class="mp-now-name">{currentFile.name}</div>
            <div class="mp-now-path">{currentShare}{currentPath}</div>
          </div>
        </div>

        <!-- Transport -->
        <div class="mp-transport">
          <button class="mp-btn" on:click={playPrev} title="Anterior">⏮</button>
          <button class="mp-btn play" on:click={togglePlay} title={playing ? 'Pausar' : 'Reproducir'}>
            {playing ? '⏸' : '▶'}
          </button>
          <button class="mp-btn" on:click={playNext} title="Siguiente">⏭</button>
        </div>

        <!-- Progress -->
        <div class="mp-progress-wrap">
          <span class="mp-time">{fmtTime(currentTime)}</span>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="mp-progress" on:click={seek}>
            <div class="mp-progress-fill" style="width:{duration ? (currentTime / duration) * 100 : 0}%"></div>
          </div>
          <span class="mp-time">{fmtTime(duration)}</span>
        </div>

        <!-- Volume -->
        <div class="mp-vol-wrap">
          <button class="mp-btn small" on:click={toggleMute} title={muted ? 'Unmute' : 'Mute'}>
            {muted || volume === 0 ? '🔇' : volume < 0.5 ? '🔉' : '🔊'}
          </button>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="mp-vol-track" on:click={setVol}>
            <div class="mp-vol-fill" style="width:{muted ? 0 : volume * 100}%"></div>
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .mp-root {
    width: 100%; height: 100%;
    display: flex; overflow: hidden;
    font-family: 'DM Sans', sans-serif;
    color: var(--text-1);
    background: var(--bg-inner);
  }

  /* ── SIDEBAR ── */
  .mp-sidebar {
    width: 200px; flex-shrink: 0;
    padding: 14px 10px;
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    overflow-y: auto;
    display: flex; flex-direction: column;
  }
  .mp-sidebar::-webkit-scrollbar { width: 3px; }
  .mp-sidebar::-webkit-scrollbar-thumb { background: rgba(128,128,128,0.15); border-radius: 2px; }

  .mp-sidebar-header { margin-bottom: 12px; }
  .mp-title { font-size: 14px; font-weight: 600; }

  .mp-section-label {
    font-size: 9px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: 0.08em;
    padding: 0 6px; margin-bottom: 4px;
  }

  .mp-share {
    padding: 6px 8px; border-radius: 6px;
    font-size: 11px; color: var(--text-2);
    cursor: pointer; transition: all 0.12s;
  }
  .mp-share:hover { background: rgba(128,128,128,0.08); color: var(--text-1); }
  .mp-share.active { background: var(--active-bg); color: var(--text-1); }

  /* ── PLAYLIST ── */
  .mp-playlist {
    display: flex; flex-direction: column; gap: 1px;
    max-height: 300px; overflow-y: auto;
  }
  .mp-pl-item {
    display: flex; align-items: center; gap: 6px;
    padding: 4px 8px; border-radius: 4px;
    font-size: 10px; color: var(--text-3);
    cursor: pointer; transition: all 0.1s;
  }
  .mp-pl-item:hover { background: rgba(128,128,128,0.06); color: var(--text-2); }
  .mp-pl-item.active { color: var(--accent); }
  .mp-pl-icon { font-size: 10px; flex-shrink: 0; }
  .mp-pl-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* ── MAIN ── */
  .mp-main {
    flex: 1; display: flex; flex-direction: column;
    overflow: hidden;
  }

  /* ── BROWSER ── */
  .mp-browser {
    flex: 1; overflow-y: auto; padding: 12px 16px;
  }
  .mp-browser.has-player { flex: 1; }
  .mp-browser::-webkit-scrollbar { width: 3px; }
  .mp-browser::-webkit-scrollbar-thumb { background: rgba(128,128,128,0.15); border-radius: 2px; }

  .mp-breadcrumb {
    display: flex; align-items: center; gap: 6px;
    margin-bottom: 10px; font-size: 11px; color: var(--text-3);
  }
  .mp-bc-back {
    font-size: 16px; cursor: pointer; color: var(--text-2);
    transition: color 0.1s; line-height: 1;
  }
  .mp-bc-back:hover { color: var(--text-1); }
  .mp-bc-path { font-family: 'DM Mono', monospace; font-size: 10px; }

  .mp-file-list { display: flex; flex-direction: column; gap: 1px; }
  .mp-file {
    display: flex; align-items: center; gap: 8px;
    padding: 7px 10px; border-radius: 6px;
    font-size: 11px; color: var(--text-2);
    transition: all 0.1s;
  }
  .mp-file.is-dir { cursor: pointer; }
  .mp-file.is-dir:hover { background: rgba(128,128,128,0.06); color: var(--text-1); }
  .mp-file.is-media { cursor: pointer; }
  .mp-file.is-media:hover { background: var(--active-bg); color: var(--text-1); }
  .mp-file.is-playing { background: var(--active-bg); color: var(--accent); }
  .mp-file-icon { font-size: 14px; flex-shrink: 0; }
  .mp-file-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mp-file-size { font-size: 10px; color: var(--text-3); font-family: 'DM Mono', monospace; flex-shrink: 0; }

  .mp-empty { font-size: 11px; color: var(--text-3); padding: 20px 0; text-align: center; }
  .mp-loading { display: flex; justify-content: center; padding: 30px; }
  .spinner {
    width: 20px; height: 20px; border-radius: 50%;
    border: 2px solid rgba(255,255,255,0.08);
    border-top-color: var(--accent);
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── VIDEO ── */
  .mp-video-wrap {
    flex: 1; display: flex; align-items: center; justify-content: center;
    background: #000; min-height: 200px;
  }
  .mp-video {
    max-width: 100%; max-height: 100%;
    object-fit: contain;
  }

  /* ── CONTROLS ── */
  .mp-controls {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 16px;
    background: var(--bg-bar);
    border-top: 1px solid var(--border);
    flex-shrink: 0;
  }

  .mp-now {
    display: flex; align-items: center; gap: 8px;
    min-width: 160px; max-width: 200px;
  }
  .mp-now-icon { font-size: 18px; flex-shrink: 0; }
  .mp-now-info { overflow: hidden; }
  .mp-now-name {
    font-size: 11px; font-weight: 600; color: var(--text-1);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .mp-now-path { font-size: 9px; color: var(--text-3); font-family: 'DM Mono', monospace; }

  .mp-transport { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }

  .mp-btn {
    width: 30px; height: 30px; border: none; background: none;
    color: var(--text-2); font-size: 14px;
    cursor: pointer; border-radius: 6px;
    display: flex; align-items: center; justify-content: center;
    transition: all 0.12s;
  }
  .mp-btn:hover { background: var(--ibtn-bg); color: var(--text-1); }
  .mp-btn.play { font-size: 18px; width: 36px; height: 36px; }
  .mp-btn.small { width: 24px; height: 24px; font-size: 12px; }

  .mp-progress-wrap {
    flex: 1; display: flex; align-items: center; gap: 8px;
  }
  .mp-time {
    font-size: 10px; color: var(--text-3);
    font-family: 'DM Mono', monospace;
    min-width: 32px; text-align: center;
  }
  .mp-progress {
    flex: 1; height: 4px; border-radius: 2px;
    background: rgba(255,255,255,0.06);
    cursor: pointer; position: relative;
  }
  :global([data-theme="light"]) .mp-progress { background: rgba(0,0,0,0.06); }
  .mp-progress-fill {
    height: 100%; border-radius: 2px;
    background: var(--accent);
    transition: width 0.1s linear;
  }

  .mp-vol-wrap {
    display: flex; align-items: center; gap: 4px;
    flex-shrink: 0;
  }
  .mp-vol-track {
    width: 60px; height: 4px; border-radius: 2px;
    background: rgba(255,255,255,0.06);
    cursor: pointer;
  }
  :global([data-theme="light"]) .mp-vol-track { background: rgba(0,0,0,0.06); }
  .mp-vol-fill {
    height: 100%; border-radius: 2px;
    background: var(--text-2);
  }
</style>
