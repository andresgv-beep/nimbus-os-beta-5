<script>
  import { pinnedApps, prefs } from '$lib/stores/theme.js';
  import { windowList, openWindow, focusWindow, restoreWindow, minimizeWindow } from '$lib/stores/windows.js';
  import { logout } from '$lib/stores/auth.js';
  import { APP_META } from '$lib/apps.js';

  function handleAppClick(appId) {
    // Check if app already open
    const existing = $windowList.find(w => w.appId === appId);
    if (existing) {
      if (existing.minimized) restoreWindow(existing.id);
      else focusWindow(existing.id);
    } else {
      openWindow(appId);
    }
  }

  function toggleMinimize(win) {
    if (win.minimized) restoreWindow(win.id);
    else minimizeWindow(win.id);
  }

  // Clock
  let time = '';
  function updateClock() {
    const now = new Date();
    const h = String(now.getHours()).padStart(2, '0');
    const m = String(now.getMinutes()).padStart(2, '0');
    time = `${h}:${m}`;
  }
  updateClock();
  setInterval(updateClock, 10000);
</script>

<div class="taskbar">
  <!-- Pinned apps -->
  <div class="pinned">
    {#each $pinnedApps as appId}
      {@const meta = APP_META[appId]}
      {#if meta}
        <button
          class="tb-icon"
          class:active={$windowList.some(w => w.appId === appId)}
          title={meta.name}
          on:click={() => handleAppClick(appId)}
        >
          <span class="tb-emoji">{meta.icon}</span>
        </button>
      {/if}
    {/each}
  </div>

  <!-- Separator -->
  <div class="sep"></div>

  <!-- Open windows -->
  <div class="open-windows">
    {#each $windowList as win}
      {@const meta = APP_META[win.appId]}
      <button
        class="tb-icon small"
        class:minimized={win.minimized}
        title={meta?.name || win.appId}
        on:click={() => toggleMinimize(win)}
      >
        <span class="tb-emoji">{meta?.icon || '📦'}</span>
      </button>
    {/each}
  </div>

  <!-- Right side -->
  <div class="right">
    <span class="clock">{time}</span>
    <button class="tb-icon" title="Logout" on:click={logout}>
      ⏻
    </button>
  </div>
</div>

<style>
  .taskbar {
    position: fixed; bottom: 0; left: 0; right: 0;
    height: var(--taskbar-height, 48px);
    background: rgba(0,0,0,0.65);
    backdrop-filter: blur(20px) saturate(1.5);
    -webkit-backdrop-filter: blur(20px) saturate(1.5);
    border-top: 1px solid rgba(255,255,255,0.08);
    display: flex; align-items: center;
    padding: 0 12px; gap: 4px;
    z-index: 9000;
  }

  .pinned, .open-windows {
    display: flex; align-items: center; gap: 2px;
  }

  .sep {
    width: 1px; height: 24px;
    background: rgba(255,255,255,0.1);
    margin: 0 6px;
  }

  .tb-icon {
    width: 40px; height: 40px;
    border-radius: 10px; border: none;
    background: transparent;
    display: flex; align-items: center; justify-content: center;
    cursor: pointer; transition: all 0.15s;
    position: relative;
  }
  .tb-icon:hover { background: rgba(255,255,255,0.1); }
  .tb-icon.active::after {
    content: ''; position: absolute; bottom: 2px;
    width: 16px; height: 3px; border-radius: 2px;
    background: var(--accent, #E95420);
  }
  .tb-icon.small { width: 34px; height: 34px; }
  .tb-icon.minimized { opacity: 0.4; }

  .tb-emoji { font-size: 20px; }

  .right {
    margin-left: auto;
    display: flex; align-items: center; gap: 8px;
  }

  .clock {
    font-size: 12px; color: rgba(255,255,255,0.7);
    font-family: 'DM Mono', monospace;
    font-weight: 500;
  }
</style>
