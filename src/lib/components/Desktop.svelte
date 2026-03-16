<script>
  import { windowList } from '$lib/stores/windows.js';
  import { prefs } from '$lib/stores/theme.js';
  import { logout } from '$lib/stores/auth.js';
  import Taskbar from './Taskbar.svelte';
  import WindowFrame from './WindowFrame.svelte';
  import WidgetLayer from './WidgetLayer.svelte';
</script>

<div class="desktop">
  <!-- Widgets (below windows) -->
  <WidgetLayer />

  <!-- Windows -->
  {#each $windowList as win (win.id)}
    {#if !win.minimized}
      <WindowFrame {win} />
    {/if}
  {/each}

  <!-- Taskbar -->
  <Taskbar />
</div>

<style>
  .desktop {
    position: fixed; inset: 0;
    background:
      radial-gradient(ellipse 80% 60% at 10% 50%, rgba(80,140,255,0.6) 0%, transparent 55%),
      radial-gradient(ellipse 60% 80% at 90% 20%, rgba(230,80,255,0.5) 0%, transparent 50%),
      linear-gradient(140deg, #1a1030 0%, #0d0d1a 100%);
    overflow: hidden;
  }
</style>
