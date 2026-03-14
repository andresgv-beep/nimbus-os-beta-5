<script>
  import { APP_META } from '$lib/apps.js';
  import { openWindow } from '$lib/stores/windows.js';

  export let visible = false;

  const apps = Object.entries(APP_META).map(([id, meta]) => ({ id, ...meta }));

  function launch(appId) {
    const meta = APP_META[appId];
    openWindow(appId, { width: meta?.width || 800, height: meta?.height || 520 });
    visible = false;
  }
</script>

{#if visible}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="overlay" on:click={() => visible = false}>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="launcher" on:click|stopPropagation>
      <div class="launcher-title">Apps</div>
      <div class="app-grid">
        {#each apps as app, i}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="app-item" on:click={() => launch(app.id)} style="animation-delay:{i * 0.03}s">
            <div class="app-icon">{app.icon}</div>
            <div class="app-name">{app.name}</div>
          </div>
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 8500;
    background: rgba(0,0,0,0.25);
    backdrop-filter: blur(4px);
  }
  .launcher {
    position: fixed;
    bottom: calc(var(--taskbar-height, 48px) + 12px);
    left: 50%;
    transform: translateX(-50%);
    width: 480px;
    background: var(--bg-frame, #111028);
    border: 1px solid var(--window-border, rgba(255,255,255,0.12));
    border-radius: 16px;
    box-shadow: var(--window-shadow, 0 32px 90px rgba(0,0,0,0.60));
    padding: 20px;
    animation: launchIn 0.25s cubic-bezier(0.16,1,0.3,1) both;
  }
  @keyframes launchIn {
    from { opacity: 0; transform: translateX(-50%) translateY(10px) scale(0.96); }
    to { opacity: 1; transform: translateX(-50%) translateY(0) scale(1); }
  }

  .launcher-title {
    font-size: 13px; font-weight: 600; color: var(--text-1);
    margin-bottom: 16px; padding: 0 4px;
    font-family: 'DM Sans', sans-serif;
  }

  .app-grid {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    gap: 6px;
  }

  .app-item {
    display: flex; flex-direction: column; align-items: center; gap: 6px;
    padding: 12px 6px 10px; border-radius: 10px;
    cursor: pointer; border: 1px solid transparent;
    transition: all 0.15s;
    animation: fadeUp 0.3s ease both;
  }
  .app-item:hover {
    background: rgba(128,128,128,0.08);
    border-color: var(--border);
  }
  @keyframes fadeUp {
    from { opacity: 0; transform: translateY(6px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .app-icon {
    font-size: 32px; line-height: 1;
    filter: drop-shadow(0 2px 6px rgba(0,0,0,0.4));
    transition: transform 0.15s;
  }
  .app-item:hover .app-icon { transform: scale(1.1) translateY(-2px); }

  .app-name {
    font-size: 10px; color: var(--text-2);
    text-align: center; font-family: 'DM Sans', sans-serif;
    max-width: 70px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
</style>
