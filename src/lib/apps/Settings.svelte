<script>
  import { prefs, setPref, THEMES, ACCENT_COLORS } from '$lib/stores/theme.js';
  import { user, logout } from '$lib/stores/auth.js';
  import TabNav from '$lib/components/TabNav.svelte';
  import StoragePanel from '$lib/apps/StoragePanel.svelte';
  import NetworkPanel from '$lib/apps/NetworkPanel.svelte';
  import SystemPanel  from '$lib/apps/SystemPanel.svelte';

  let activeView    = 'system';
  let appearanceTab = 'tema';
  let storageTab    = 'disks';
  let networkTab    = 'interfaces';
  let networkSub    = 'interfaces';
  let systemTab     = 'monitor';
  let systemSub     = 'monitor';

  // Network subsection defaults per tab
  const networkSubDefaults = { interfaces:'interfaces', services:'smb', remoteaccess:'ports', security:'firewall' };
  const systemSubDefaults  = { monitor:'monitor', users:'users', permissions:'sharefolders', portal:'portal', updates:'updates' };

  let prevNetworkTab = networkTab;
  let prevSystemTab  = systemTab;
  $: if (networkTab !== prevNetworkTab) { prevNetworkTab = networkTab; networkSub = networkSubDefaults[networkTab] ?? networkTab; }
  $: if (systemTab  !== prevSystemTab)  { prevSystemTab  = systemTab;  systemSub  = systemSubDefaults[systemTab]  ?? systemTab; }

  const sidebarItems = [
    { id: 'system',     section: 'Sistema',      label: 'Sistema',    icon: 'grid'   },
    { id: 'storage',    section: 'Sistema',      label: 'Storage',    icon: 'db'     },
    { id: 'network',    section: 'Sistema',      label: 'Red',        icon: 'net'    },
    { id: 'security',   section: 'Sistema',      label: 'Seguridad',  icon: 'shield' },
    { id: 'appearance', section: 'Preferencias', label: 'Apariencia', icon: 'eye'    },
    { id: 'about',      section: 'Preferencias', label: 'Acerca de',  icon: 'clock'  },
  ];

  const themeLabels = { midnight: 'Midnight', dark: 'Dark', light: 'Light' };
  function setTheme(t)  { setPref('theme', t); }
  function setAccent(n) { setPref('accentColor', n); }

  $: currentTheme  = $prefs.theme       || 'midnight';
  $: currentAccent = $prefs.accentColor || 'orange';
  $: userName      = $user?.username    || 'User';
  $: userRole      = $user?.role        || 'user';

  // Subtitle label
  const viewLabel = { system:'Sistema', storage:'Storage', network:'Red', security:'Seguridad', appearance:'Apariencia', about:'Acerca de' };
</script>

<div class="settings-root">
  <!-- SIDEBAR — always flat, no drill-down -->
  <div class="sidebar">
    <div class="sb-header">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color:var(--accent);flex-shrink:0">
        <rect x="2" y="3" width="9" height="9" rx="2"/><rect x="13" y="3" width="9" height="9" rx="2"/>
        <rect x="2" y="13" width="9" height="9" rx="2"/><rect x="13" y="13" width="9" height="9" rx="2"/>
      </svg>
      <span class="sb-app-title">Settings</span>
    </div>

    <div class="sb-search">⌕ Buscar…</div>

    <div class="sb-section">Sistema</div>
    {#each sidebarItems.filter(i => i.section === 'Sistema') as item}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={activeView === item.id} on:click={() => activeView = item.id}>
        <span class="sb-ico">
          {#if item.icon === 'grid'}⊞{:else if item.icon === 'db'}⛁{:else if item.icon === 'net'}⬡{:else if item.icon === 'shield'}⛨{:else}●{/if}
        </span>
        {item.label}
      </div>
    {/each}

    <div class="sb-section" style="margin-top:8px">Preferencias</div>
    {#each sidebarItems.filter(i => i.section === 'Preferencias') as item}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={activeView === item.id} on:click={() => activeView = item.id}>
        <span class="sb-ico">
          {#if item.icon === 'eye'}◉{:else if item.icon === 'clock'}◷{:else}●{/if}
        </span>
        {item.label}
      </div>
    {/each}

    <div class="sb-bottom">
      <div class="sb-user-card">
        <div class="sb-avatar">{userName[0].toUpperCase()}</div>
        <div class="sb-user-info">
          <div class="sb-user-name">{userName}</div>
          <div class="sb-user-role">{userRole}</div>
        </div>
      </div>
    </div>
  </div>

  <!-- INNER -->
  <div class="inner-wrap">
    <div class="inner">
      <!-- TITLEBAR -->
      <div class="inner-titlebar">
        <div class="tb-title">NimSettings</div>
        <div class="tb-sub">— {viewLabel[activeView] || ''}</div>

        {#if activeView === 'appearance'}
          <div class="tb-tabs">
            <TabNav tabs={[
              { id:'tema',    label:'Tema'     },
              { id:'fondos',  label:'Fondos'   },
              { id:'widgets', label:'Widgets'  },
              { id:'taskbar', label:'Taskbar'  },
            ]} bind:active={appearanceTab} />
          </div>
        {:else if activeView === 'storage'}
          <div class="tb-tabs">
            <TabNav tabs={[
              { id:'disks',   label:'Disks'            },
              { id:'pools',   label:'Storage Manager'  },
              { id:'health',  label:'Health'           },
              { id:'restore', label:'Restore Pool'     },
            ]} bind:active={storageTab} />
          </div>
        {:else if activeView === 'network'}
          <div class="tb-tabs">
            <TabNav tabs={[
              { id:'interfaces',   label:'Interfaces'    },
              { id:'services',     label:'Services'      },
              { id:'remoteaccess', label:'Remote Access' },
              { id:'security',     label:'Security'      },
            ]} bind:active={networkTab} />
          </div>
        {:else if activeView === 'system'}
          <div class="tb-tabs">
            <TabNav tabs={[
              { id:'monitor',     label:'Monitor'     },
              { id:'users',       label:'Users'       },
              { id:'permissions', label:'Permissions' },
              { id:'portal',      label:'Portal'      },
              { id:'updates',     label:'Updates'     },
            ]} bind:active={systemTab} />
          </div>
        {/if}
      </div>

      <!-- CONTENT -->
      <div class="inner-content" class:no-pad={activeView === 'storage' || activeView === 'network' || activeView === 'system'}>

        {#if activeView === 'appearance'}
          {#if appearanceTab === 'tema'}
            <div class="section-label">Tema del sistema</div>
            <div class="theme-row">
              {#each ['midnight', 'dark', 'light'] as t}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="theme-card" class:active={currentTheme === t} on:click={() => setTheme(t)}>
                  <div class="theme-preview {t}">
                    <div class="tp-sidebar"></div>
                    <div class="tp-content">
                      <div class="tp-bar"></div>
                      <div class="tp-line"></div>
                      <div class="tp-line short"></div>
                    </div>
                  </div>
                  <div class="theme-label">{themeLabels[t]}</div>
                </div>
              {/each}
            </div>
            <div class="section-label" style="margin-top:24px">Color de acento</div>
            <div class="accent-row">
              {#each Object.entries(ACCENT_COLORS) as [name, color]}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="accent-dot" class:active={currentAccent === name}
                  style="background:{color}" on:click={() => setAccent(name)} title={name}></div>
              {/each}
            </div>
          {:else if appearanceTab === 'fondos'}
            <div class="section-label">Fondos de escritorio</div>
            <p style="color:var(--text-3);font-size:12px">Wallpaper selector — coming soon</p>
          {:else if appearanceTab === 'widgets'}
            <div class="section-label">Widgets del escritorio</div>
            <p style="color:var(--text-3);font-size:12px">Widget configuration — coming soon</p>
          {:else if appearanceTab === 'taskbar'}
            <div class="section-label">Modo</div>
            <div class="setting-row">
              <span class="setting-label">Estilo</span>
              <div class="setting-options">
                <button class="opt-btn" class:active={$prefs.taskbarMode === 'classic'} on:click={() => setPref('taskbarMode', 'classic')}>
                  Clásico
                </button>
                <button class="opt-btn" class:active={$prefs.taskbarMode === 'dock'} on:click={() => setPref('taskbarMode', 'dock')}>
                  Dock
                </button>
              </div>
            </div>
            <div class="section-label" style="margin-top:16px">Posición</div>
            <div class="setting-row">
              <span class="setting-label">Posición</span>
              <div class="setting-options">
                {#each ['bottom', 'top', 'left'] as pos}
                  <button class="opt-btn"
                    class:active={$prefs.taskbarPosition === pos}
                    disabled={$prefs.taskbarMode === 'dock' && pos === 'left'}
                    on:click={() => setPref('taskbarPosition', pos)}>
                    {pos === 'bottom' ? 'Abajo' : pos === 'top' ? 'Arriba' : 'Izquierda'}
                  </button>
                {/each}
              </div>
            </div>
            <div class="setting-row">
              <span class="setting-label">Tamaño</span>
              <div class="setting-options">
                {#each ['small', 'medium', 'large'] as size}
                  <button class="opt-btn" class:active={$prefs.taskbarSize === size} on:click={() => setPref('taskbarSize', size)}>
                    {size === 'small' ? 'Pequeño' : size === 'medium' ? 'Medio' : 'Grande'}
                  </button>
                {/each}
              </div>
            </div>
          {/if}

        {:else if activeView === 'system'}
          <SystemPanel activeTab={systemTab} bind:activeSub={systemSub} />

        {:else if activeView === 'storage'}
          <StoragePanel activeTab={storageTab} />

        {:else if activeView === 'network'}
          <NetworkPanel activeTab={networkTab} bind:activeSub={networkSub} />

        {:else if activeView === 'security'}
          <div class="section-label">Seguridad</div>
          <p style="color:var(--text-3);font-size:12px">Security panel — coming soon</p>

        {:else if activeView === 'about'}
          <div class="section-label">Acerca de NimOS</div>
          <p style="color:var(--text-2);font-size:12px">NimOS Beta 5 — NAS Operating System</p>
          <p style="color:var(--text-3);font-size:11px;margin-top:4px">Backend: Go · Frontend: SvelteKit · License: MIT</p>
        {/if}
      </div>

      <div class="statusbar">
        <div class="status-dot"></div>
        <span>NimOS Beta 5</span>
      </div>
    </div>
  </div>
</div>
<style>
  .settings-root {
    width:100%; height:100%;
    display:flex; overflow:hidden;
    font-family:'DM Sans',-apple-system,sans-serif;
    color:var(--text-1);
  }

  /* ── SIDEBAR ── */
  .sidebar {
    width:200px; flex-shrink:0;
    display:flex; flex-direction:column;
    padding:12px 8px;
    background:var(--bg-sidebar);
  }
  .sb-header {
    display:flex; align-items:center; gap:9px;
    padding:18px 10px 14px;
  }
  .sb-app-title { font-size:16px; font-weight:700; color:var(--text-1); }

  .sb-bottom { margin-top:auto; border-top:1px solid var(--border); padding-top:8px; }
  .sb-user-card {
    display:flex; align-items:center; gap:10px;
    padding:10px 10px;
  }
  .sb-avatar {
    width:30px; height:30px; border-radius:8px; flex-shrink:0;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    display:flex; align-items:center; justify-content:center;
    font-size:12px; font-weight:700; color:#fff;
  }
  .sb-user-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .sb-user-role { font-size:10px; color:var(--text-3); text-transform:uppercase; letter-spacing:.04em; }
  .sb-search {
    display:flex; align-items:center; gap:6px;
    padding:6px 10px; border-radius:8px; margin-bottom:10px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    font-size:11px; color:var(--text-3);
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
  .sb-ico { font-size:13px; width:16px; text-align:center; flex-shrink:0; opacity:0.7; }
  .sb-item.active .sb-ico { opacity:1; }

  .sb-back {
    display:flex; align-items:center; gap:6px;
    padding:7px 10px; border-radius:8px; cursor:pointer;
    font-size:12px; font-weight:600; color:var(--text-2);
    transition:all .15s; margin-bottom:2px;
  }
  .sb-back:hover { color:var(--text-1); background:rgba(128,128,128,0.08); }
  .sb-back-arrow { font-size:16px; line-height:1; color:var(--text-3); }
  .sb-divider { height:1px; background:var(--border); margin:6px 4px 8px; }
  .sb-bottom { margin-top:auto; border-top:1px solid var(--border); padding-top:8px; }

  /* ── INNER ── */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner {
    flex:1; border-radius:10px; border:1px solid var(--border);
    background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden;
  }
  .inner-titlebar {
    display:flex; align-items:center; gap:8px;
    padding:14px 16px 12px; background:var(--bg-bar); flex-shrink:0;
  }
  .tb-title { font-size:13px; font-weight:600; color:var(--text-1); }
  .tb-sub { font-size:11px; color:var(--text-3); margin-left:2px; }
  .tb-tabs { margin-left:auto; }
  .inner-content { flex:1; overflow-y:auto; padding:20px; }
  .inner-content.no-pad { padding:0; overflow:hidden; display:flex; flex-direction:column; }
  .inner-content::-webkit-scrollbar { width:3px; }
  .inner-content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .section-label {
    font-size:10px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.08em; margin-bottom:12px;
  }

  /* ── SYSTEM GRID ── */
  .app-grid { display:grid; grid-template-columns:repeat(4,1fr); gap:10px; margin-bottom:24px; }
  .app-tile {
    display:flex; flex-direction:column; align-items:center; gap:8px;
    padding:16px 12px; border-radius:10px; cursor:pointer;
    border:1px solid var(--border); background:var(--ibtn-bg);
    transition:all .2s; animation:fadeUp .3s ease both;
  }
  .app-tile:hover { background:var(--active-bg); border-color:var(--border-hi); transform:translateY(-1px); }
  @keyframes fadeUp { from{opacity:0;transform:translateY(6px)} to{opacity:1;transform:translateY(0)} }
  .tile-icon { font-size:24px; }
  .tile-label { font-size:11px; font-weight:500; color:var(--text-2); text-align:center; }

  /* ── APPEARANCE: THEME CARDS ── */
  .theme-row { display:flex; gap:12px; }
  .theme-card {
    cursor:pointer; display:flex; flex-direction:column; align-items:center; gap:8px;
    padding:8px; border-radius:10px; border:2px solid transparent;
    transition:all .2s;
  }
  .theme-card:hover { border-color:var(--border); }
  .theme-card.active { border-color:var(--accent); }
  .theme-preview {
    width:120px; height:72px; border-radius:8px; overflow:hidden;
    display:flex; border:1px solid rgba(128,128,128,0.15);
  }
  .theme-preview.midnight { background:#111028; }
  .theme-preview.dark { background:#181818; }
  .theme-preview.light { background:#ebebef; }
  .tp-sidebar { width:30%; height:100%; }
  .theme-preview.midnight .tp-sidebar { background:#0d0b20; }
  .theme-preview.dark .tp-sidebar { background:#141414; }
  .theme-preview.light .tp-sidebar { background:#e0e0e4; }
  .tp-content { flex:1; padding:8px; display:flex; flex-direction:column; gap:4px; }
  .tp-bar { height:4px; border-radius:2px; width:60%; }
  .theme-preview.midnight .tp-bar { background:rgba(124,111,255,0.4); }
  .theme-preview.dark .tp-bar { background:rgba(124,111,255,0.35); }
  .theme-preview.light .tp-bar { background:rgba(91,79,240,0.3); }
  .tp-line { height:3px; border-radius:2px; width:80%; }
  .tp-line.short { width:50%; }
  .theme-preview.midnight .tp-line { background:rgba(255,255,255,0.08); }
  .theme-preview.dark .tp-line { background:rgba(255,255,255,0.07); }
  .theme-preview.light .tp-line { background:rgba(0,0,0,0.06); }
  .theme-label { font-size:11px; font-weight:500; color:var(--text-2); }
  .theme-card.active .theme-label { color:var(--text-1); }

  /* ── APPEARANCE: ACCENT DOTS ── */
  .accent-row { display:flex; gap:10px; }
  .accent-dot {
    width:28px; height:28px; border-radius:50%; cursor:pointer;
    border:2px solid transparent; transition:all .15s;
    box-shadow:0 2px 8px rgba(0,0,0,0.3);
  }
  .accent-dot:hover { transform:scale(1.15); }
  .accent-dot.active { border-color:var(--text-1); transform:scale(1.15); }

  /* ── APPEARANCE: SETTINGS ROWS ── */
  .setting-row {
    display:flex; align-items:center; justify-content:space-between;
    padding:10px 0; border-bottom:1px solid var(--border);
  }
  .setting-label { font-size:12px; color:var(--text-2); }
  .setting-options { display:flex; gap:4px; }
  .opt-btn {
    padding:5px 12px; border-radius:6px; font-size:11px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    color:var(--text-2); cursor:pointer; font-family:inherit;
    transition:all .15s;
  }
  .opt-btn:hover { color:var(--text-1); }
  .opt-btn.active { background:var(--active-bg); border-color:var(--border-hi); color:var(--text-1); }

  /* ── STATUSBAR ── */
  .statusbar {
    display:flex; align-items:center; gap:12px;
    padding:8px 16px; border-top:1px solid var(--border);
    background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3);
    border-radius:0 0 10px 10px; font-family:'DM Mono',monospace;
  }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,0.6); }
</style>
