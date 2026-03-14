<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let activeTab = 'monitor';
  export let activeSub = 'monitor';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  // ── Data ──
  let systemData  = {};
  let users       = [];
  let shares      = [];
  let portalData  = {};
  let updateData  = {};
  let loading     = false;

  async function loadTab(tab) {
    loading = true;
    try {
      if (tab === 'monitor') {
        const r = await fetch('/api/system', { headers: hdrs() });
        systemData = await r.json();
      } else if (tab === 'users') {
        const r = await fetch('/api/users', { headers: hdrs() });
        const d = await r.json();
        users = d.users || d || [];
      } else if (tab === 'permissions') {
        const r = await fetch('/api/shares', { headers: hdrs() });
        const d = await r.json();
        shares = d.shares || d || [];
      } else if (tab === 'updates') {
        const r = await fetch('/api/system/update/status', { headers: hdrs() });
        updateData = await r.json();
      }
    } catch(e) { console.error('[System] load failed', e); }
    loading = false;
  }

  $: loadTab(activeTab);
</script>

<div class="sys-root">
  <div class="sys-content">
    {#if loading}
      <div class="s-loading"><div class="spinner"></div></div>

    <!-- ══ MONITOR ══ -->
    {:else if activeSub === 'monitor'}
      <div class="section-label">Monitor del sistema</div>
      <p class="coming-soon">Dashboard — coming soon</p>

    <!-- ══ USERS ══ -->
    {:else if activeSub === 'users'}
      <div class="section-label">Usuarios</div>
      {#if users.length === 0}
        <p class="coming-soon">No hay usuarios</p>
      {:else}
        <div class="user-list">
          {#each users as u}
            <div class="user-row">
              <div class="user-avatar">{(u.username || u.name || '?')[0].toUpperCase()}</div>
              <div class="user-info">
                <div class="user-name">{u.username || u.name}</div>
                <div class="user-role">{u.role || 'user'}</div>
              </div>
              <div class="user-badge" class:admin={u.role === 'admin'}>{u.role || 'user'}</div>
            </div>
          {/each}
        </div>
      {/if}

    <!-- ══ PERMISSIONS: SHARED FOLDERS ══ -->
    {:else if activeSub === 'sharefolders'}
      <div class="section-label">Carpetas compartidas</div>
      {#if shares.length === 0}
        <p class="coming-soon">Sin carpetas configuradas</p>
      {:else}
        <div class="share-list">
          {#each shares as s}
            <div class="share-row">
              <div class="share-icon">📁</div>
              <div class="share-info">
                <div class="share-name">{s.name}</div>
                <div class="share-path">{s.path}</div>
              </div>
              <div class="share-perms">
                {#if s.public}<span class="perm-tag">Público</span>{/if}
                {#if s.readonly}<span class="perm-tag warn">Solo lectura</span>{/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}

    <!-- ══ PERMISSIONS: APP PERMISSIONS ══ -->
    {:else if activeSub === 'apppermissions'}
      <div class="section-label">Permisos de aplicaciones</div>
      <p class="coming-soon">App permissions — coming soon</p>

    <!-- ══ PORTAL ══ -->
    {:else if activeSub === 'portal'}
      <div class="section-label">Portal de acceso</div>
      <p class="coming-soon">Portal configuration — coming soon</p>

    <!-- ══ UPDATES ══ -->
    {:else if activeSub === 'updates'}
      <div class="section-label">Actualizaciones</div>
      <div class="field-group">
        <div class="field-row">
          <span class="field-label">Versión actual</span>
          <span class="field-value">{updateData.current || updateData.version || '—'}</span>
        </div>
        <div class="field-row">
          <span class="field-label">Última comprobación</span>
          <span class="field-value">{updateData.lastCheck || '—'}</span>
        </div>
        <div class="field-row">
          <span class="field-label">Estado</span>
          <span class="field-value" style="color:{updateData.available ? 'var(--amber)' : 'var(--green)'}">
            {updateData.available ? 'Actualización disponible' : 'Al día'}
          </span>
        </div>
      </div>
      {#if updateData.available}
        <div class="update-card">
          <div class="update-version">{updateData.latest || 'Nueva versión'}</div>
          <div class="update-notes">{updateData.notes || ''}</div>
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .sys-root { width:100%; height:100%; display:flex; overflow:hidden; }
  .sys-content { flex:1; overflow-y:auto; padding:18px 20px; }
  .sys-content::-webkit-scrollbar { width:3px; }
  .sys-content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .s-loading { display:flex; align-items:center; justify-content:center; height:100%; }
  .spinner {
    width:24px; height:24px; border-radius:50%;
    border:2px solid rgba(255,255,255,0.08);
    border-top-color:var(--accent);
    animation:spin .7s linear infinite;
  }
  @keyframes spin { to { transform:rotate(360deg); } }

  .section-label {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.08em; margin-bottom:12px;
  }
  .coming-soon { font-size:12px; color:var(--text-3); }

  /* ── USERS ── */
  .user-list { display:flex; flex-direction:column; gap:6px; }
  .user-row {
    display:flex; align-items:center; gap:10px;
    padding:10px 12px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .user-avatar {
    width:28px; height:28px; border-radius:7px; flex-shrink:0;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    display:flex; align-items:center; justify-content:center;
    font-size:11px; font-weight:700; color:#fff;
  }
  .user-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .user-role { font-size:10px; color:var(--text-3); text-transform:uppercase; letter-spacing:.04em; }
  .user-badge {
    margin-left:auto; padding:2px 8px; border-radius:4px;
    font-size:9px; font-weight:600; text-transform:uppercase;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3);
  }
  .user-badge.admin { background:rgba(124,111,255,0.12); border-color:rgba(124,111,255,0.30); color:var(--accent); }

  /* ── SHARES ── */
  .share-list { display:flex; flex-direction:column; gap:6px; }
  .share-row {
    display:flex; align-items:center; gap:10px;
    padding:10px 12px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .share-icon { font-size:16px; flex-shrink:0; }
  .share-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .share-path { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:1px; }
  .share-perms { margin-left:auto; display:flex; gap:5px; }
  .perm-tag {
    padding:2px 7px; border-radius:4px; font-size:9px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3);
  }
  .perm-tag.warn { background:rgba(251,191,36,0.10); border-color:rgba(251,191,36,0.25); color:var(--amber); }

  /* ── FIELDS ── */
  .field-group { display:flex; flex-direction:column; }
  .field-row {
    display:flex; align-items:center; justify-content:space-between;
    padding:8px 0; border-bottom:1px solid var(--border);
  }
  .field-label { font-size:11px; color:var(--text-2); }
  .field-value { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; }

  /* ── UPDATES ── */
  .update-card {
    margin-top:12px; padding:12px 14px; border-radius:8px;
    border:1px solid rgba(251,191,36,0.25); background:rgba(251,191,36,0.06);
  }
  .update-version { font-size:12px; font-weight:600; color:var(--amber); margin-bottom:4px; }
  .update-notes { font-size:11px; color:var(--text-2); }
</style>
