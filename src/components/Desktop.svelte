<script>
  import { windowList } from '$lib/stores/windows.js';
  import { prefs } from '$lib/stores/theme.js';
  import { logout } from '$lib/stores/auth.js';
  import Taskbar from './Taskbar.svelte';
  import WindowFrame from './WindowFrame.svelte';
</script>

<div class="desktop">
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
    background: var(--wallpaper);
    overflow: hidden;
  }
</style>
