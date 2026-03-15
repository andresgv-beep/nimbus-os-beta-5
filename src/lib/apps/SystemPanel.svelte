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
  let pools       = [];
  let portalData  = {};
  let updateData  = {};
  let loading     = false;
  let checking    = false;
  let applying    = false;
  let updateMsg   = '';
  let updateMsgError = false;
  let updatePollId = null;

  // ── Shared Folders state ──
  let editingShare = null;
  let savingShare  = false;
  let shareMsg     = '';
  let shareMsgError = false;

  function startNewShare() {
    const defaultPool = pools.length > 0 ? pools[0].name : '';
    editingShare = {
      _isNew: true,
      name: '',
      description: '',
      pool: defaultPool,
      _perms: {},
    };
    // Default: admin users get rw
    for (const u of users) {
      if (u.role === 'admin') editingShare._perms[u.username] = 'rw';
    }
    shareMsg = '';
  }

  function startEditShare(s) {
    const perms = {};
    if (s.permissions) {
      for (const [u, p] of Object.entries(s.permissions)) {
        perms[u] = p;
      }
    }
    editingShare = {
      _isNew: false,
      name: s.name,
      displayName: s.displayName,
      description: s.description || '',
      pool: s.pool,
      _perms: perms,
    };
    shareMsg = '';
  }

  async function saveShare() {
    savingShare = true; shareMsg = '';
    try {
      if (editingShare._isNew) {
        if (!editingShare.name.trim()) { shareMsg = 'Nombre requerido'; shareMsgError = true; savingShare = false; return; }
        const res = await fetch('/api/shares', {
          method: 'POST',
          headers: { ...hdrs(), 'Content-Type': 'application/json' },
          body: JSON.stringify({
            name: editingShare.name.trim(),
            description: editingShare.description,
            pool: editingShare.pool,
          }),
        });
        const data = await res.json();
        if (!data.ok) { shareMsg = data.error || 'Error al crear'; shareMsgError = true; savingShare = false; return; }
        // Now set permissions
        await fetch(`/api/shares/${data.name}`, {
          method: 'PUT',
          headers: { ...hdrs(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ permissions: editingShare._perms }),
        });
        shareMsg = `"${editingShare.name}" creada`; shareMsgError = false;
      } else {
        // Update existing
        const res = await fetch(`/api/shares/${editingShare.name}`, {
          method: 'PUT',
          headers: { ...hdrs(), 'Content-Type': 'application/json' },
          body: JSON.stringify({
            description: editingShare.description,
            permissions: editingShare._perms,
          }),
        });
        const data = await res.json();
        if (!data.ok) { shareMsg = data.error || 'Error al guardar'; shareMsgError = true; savingShare = false; return; }
        shareMsg = 'Permisos actualizados'; shareMsgError = false;
      }
      editingShare = null;
      loadTab('permissions');
    } catch (e) { shareMsg = 'Error de conexión'; shareMsgError = true; }
    savingShare = false;
  }

  async function deleteShare(name) {
    if (!confirm(`¿Eliminar la carpeta compartida "${name}"? Los archivos se conservan.`)) return;
    try {
      const res = await fetch(`/api/shares/${name}`, { method: 'DELETE', headers: hdrs() });
      const data = await res.json();
      if (data.ok) { loadTab('permissions'); }
      else { alert(data.error || 'Error al eliminar'); }
    } catch (e) { alert('Error de conexión'); }
  }

  async function checkForUpdates() {
    checking = true; updateMsg = '';
    try {
      const r = await fetch('/api/system/update/check', { headers: hdrs() });
      const d = await r.json();
      updateData = { ...updateData, ...d };
      if (d.updateAvailable) { updateMsg = `Versión ${d.latestVersion} disponible`; updateMsgError = false; }
      else { updateMsg = 'Ya estás en la última versión'; updateMsgError = false; }
    } catch (e) { updateMsg = 'Error comprobando'; updateMsgError = true; }
    checking = false;
  }

  async function applyUpdate() {
    applying = true; updateMsg = 'Aplicando actualización...';
    try {
      const r = await fetch('/api/system/update/apply', { method: 'POST', headers: hdrs() });
      const d = await r.json();
      if (d.ok) {
        updateMsg = 'Actualización en progreso. Espera...';
        updateMsgError = false;
        // Poll status
        updatePollId = setInterval(async () => {
          try {
            const sr = await fetch('/api/system/update/status', { headers: hdrs() });
            const sd = await sr.json();
            if (sd.done) {
              clearInterval(updatePollId);
              applying = false;
              if (sd.type === 'error') { updateMsg = sd.error || 'Error en la actualización'; updateMsgError = true; }
              else { updateMsg = `Actualizado: ${sd.prev || '?'} → ${sd.new || '?'}. Recarga el navegador.`; updateMsgError = false; }
              updateData = sd;
            }
          } catch {}
        }, 3000);
      } else { updateMsg = d.error || 'Error'; updateMsgError = true; applying = false; }
    } catch (e) { updateMsg = 'Error de conexión'; updateMsgError = true; applying = false; }
  }

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
        const [sr, ur, pr] = await Promise.all([
          fetch('/api/shares', { headers: hdrs() }),
          fetch('/api/users', { headers: hdrs() }),
          fetch('/api/storage/pools', { headers: hdrs() }),
        ]);
        const sd = await sr.json();
        const ud = await ur.json();
        const pd = await pr.json();
        shares = sd.shares || sd || [];
        users  = ud.users  || ud || [];
        pools  = Array.isArray(pd) ? pd : (pd.pools || []);
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

      {#if shares.length > 0}
        <div class="share-list">
          {#each shares as s}
            <div class="share-row">
              <div class="share-icon">📁</div>
              <div class="share-info">
                <div class="share-name">{s.displayName || s.name}</div>
                <div class="share-path">{s.path}</div>
                <div class="share-meta">
                  {s.pool || '—'} · {Object.keys(s.permissions || {}).length} usuario{Object.keys(s.permissions || {}).length !== 1 ? 's' : ''}
                  {#if s.description}<span class="share-desc"> · {s.description}</span>{/if}
                </div>
              </div>
              <div class="share-actions">
                <button class="share-action-btn" on:click={() => startEditShare(s)} title="Editar permisos">✎</button>
                <button class="share-action-btn danger" on:click={() => deleteShare(s.name)} title="Eliminar">✕</button>
              </div>
            </div>
          {/each}
        </div>
        <div class="pool-sep"></div>
      {/if}

      <!-- Create / Edit form -->
      {#if editingShare}
        <div class="section-label" style="margin-top:8px">{editingShare._isNew ? 'Crear carpeta compartida' : `Editar: ${editingShare.displayName || editingShare.name}`}</div>
        <div class="share-form">
          {#if editingShare._isNew}
            <div class="form-field">
              <label class="form-label">Nombre</label>
              <input class="form-input" type="text" placeholder="documentos" bind:value={editingShare.name} />
            </div>

            <div class="form-field">
              <label class="form-label">Descripción</label>
              <input class="form-input" type="text" placeholder="Archivos compartidos del equipo" bind:value={editingShare.description} />
            </div>

            <div class="form-field">
              <label class="form-label">Pool de almacenamiento</label>
              <select class="form-select" bind:value={editingShare.pool}>
                {#each pools as pool}
                  <option value={pool.name}>{pool.name} — {pool.totalFormatted || '—'} ({pool.raidLevel})</option>
                {/each}
              </select>
            </div>
          {/if}

          <!-- User Permissions -->
          <div class="form-field">
            <label class="form-label">Permisos de usuario</label>
            <div class="perm-table">
              <div class="perm-header">
                <span class="perm-col-user">Usuario</span>
                <span class="perm-col-perm">Permiso</span>
              </div>
              {#each users as u}
                <div class="perm-row">
                  <div class="perm-col-user">
                    <span class="perm-user-avatar">{(u.username || '?')[0].toUpperCase()}</span>
                    <span class="perm-user-name">{u.username}</span>
                    {#if u.role === 'admin'}<span class="perm-admin-tag">admin</span>{/if}
                  </div>
                  <div class="perm-col-perm">
                    <select class="form-select perm-select"
                      value={editingShare._perms[u.username] || 'none'}
                      on:change={(e) => { editingShare._perms[u.username] = e.target.value; editingShare = editingShare; }}>
                      <option value="none">Sin acceso</option>
                      <option value="ro">Solo lectura</option>
                      <option value="rw">Lectura / Escritura</option>
                    </select>
                  </div>
                </div>
              {/each}
            </div>
          </div>

          <div class="form-actions">
            <button class="btn-accent" on:click={saveShare} disabled={savingShare}>
              {savingShare ? 'Guardando...' : editingShare._isNew ? 'Crear carpeta' : 'Guardar cambios'}
            </button>
            <button class="btn-secondary" on:click={() => editingShare = null}>Cancelar</button>
          </div>

          {#if shareMsg}
            <div class="share-msg" class:error={shareMsgError}>{shareMsg}</div>
          {/if}
        </div>
      {:else}
        {#if pools.length > 0}
          <button class="btn-accent" style="margin-top:4px" on:click={startNewShare}>+ Nueva carpeta compartida</button>
        {:else}
          <p class="coming-soon" style="margin-top:8px">Crea un pool de almacenamiento en Storage Manager primero.</p>
        {/if}
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
          <span class="field-value">{updateData.currentVersion || updateData.current || updateData.version || '—'}</span>
        </div>
        <div class="field-row">
          <span class="field-label">Última versión</span>
          <span class="field-value">{updateData.latestVersion || updateData.latest || '—'}</span>
        </div>
        <div class="field-row">
          <span class="field-label">Estado</span>
          <span class="field-value" style="color:{updateData.updateAvailable ? 'var(--amber)' : 'var(--green)'}">
            {updateData.updateAvailable ? 'Actualización disponible' : 'Al día'}
          </span>
        </div>
      </div>

      <div class="update-actions">
        <button class="btn-secondary" on:click={checkForUpdates} disabled={checking || applying}>
          {checking ? 'Comprobando...' : 'Comprobar actualizaciones'}
        </button>
        {#if updateData.updateAvailable}
          <button class="btn-accent" on:click={applyUpdate} disabled={applying}>
            {applying ? 'Actualizando...' : 'Aplicar actualización'}
          </button>
        {/if}
      </div>

      {#if updateMsg}
        <div class="update-msg" class:error={updateMsgError}>{updateMsg}</div>
      {/if}

      {#if applying}
        <div class="update-progress">
          <div class="spinner" style="width:18px;height:18px"></div>
          <span>No cierres el navegador</span>
        </div>
      {/if}

      {#if updateData.type === 'full'}
        <div class="update-card">
          <span>✓ Daemon recompilado y reiniciado</span>
        </div>
      {:else if updateData.type === 'frontend'}
        <div class="update-card">
          <span>✓ Frontend actualizado — recarga el navegador</span>
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
  .share-meta { font-size:10px; color:var(--text-3); margin-top:2px; }
  .share-desc { color:var(--text-3); }
  .share-actions { margin-left:auto; display:flex; gap:4px; flex-shrink:0; }
  .share-action-btn {
    width:26px; height:26px; border-radius:6px; border:1px solid var(--border);
    background:var(--ibtn-bg); color:var(--text-3); font-size:11px;
    cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:all .15s;
  }
  .share-action-btn:hover { color:var(--text-1); border-color:var(--border-hi); }
  .share-action-btn.danger:hover { color:var(--red); border-color:rgba(248,113,113,0.25); }
  .share-perms { margin-left:auto; display:flex; gap:5px; }
  .perm-tag {
    padding:2px 7px; border-radius:4px; font-size:9px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3);
  }
  .perm-tag.warn { background:rgba(251,191,36,0.10); border-color:rgba(251,191,36,0.25); color:var(--amber); }

  /* ── SHARE FORM ── */
  .share-form { display:flex; flex-direction:column; gap:14px; max-width:520px; }
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input, .form-select {
    padding:9px 12px; border-radius:8px;
    background:rgba(255,255,255,0.04); border:1px solid var(--border);
    color:var(--text-1); font-size:12px; font-family:'DM Sans',sans-serif;
    outline:none; transition:border-color .2s;
  }
  .form-input:focus, .form-select:focus { border-color:var(--accent); }
  .form-input::placeholder { color:var(--text-3); }
  .form-select { cursor:pointer; -webkit-appearance:none; appearance:none;
    background-image:url("data:image/svg+xml,%3Csvg width='10' height='6' viewBox='0 0 10 6' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23666' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E");
    background-repeat:no-repeat; background-position:right 12px center; padding-right:32px;
  }
  .form-select option { background:var(--bg-inner); color:var(--text-1); }
  .form-actions { display:flex; gap:8px; margin-top:4px; }
  .share-msg { font-size:11px; padding:6px 0; color:var(--green); }
  .share-msg.error { color:var(--red); }
  .pool-sep { height:1px; background:var(--border); margin:12px 0; }

  /* ── PERMISSION TABLE ── */
  .perm-table { display:flex; flex-direction:column; gap:2px; }
  .perm-header {
    display:flex; align-items:center; padding:4px 8px;
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.06em;
  }
  .perm-row {
    display:flex; align-items:center; gap:8px;
    padding:7px 8px; border-radius:6px;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .perm-col-user { display:flex; align-items:center; gap:8px; flex:1; min-width:0; }
  .perm-col-perm { flex-shrink:0; }
  .perm-user-avatar {
    width:22px; height:22px; border-radius:5px; flex-shrink:0;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    display:flex; align-items:center; justify-content:center;
    font-size:9px; font-weight:700; color:#fff;
  }
  .perm-user-name { font-size:11px; font-weight:600; color:var(--text-1); }
  .perm-admin-tag {
    font-size:8px; font-weight:600; text-transform:uppercase; letter-spacing:.04em;
    padding:1px 5px; border-radius:3px;
    background:rgba(124,111,255,0.12); color:var(--accent);
  }
  .perm-select { padding:5px 28px 5px 8px; font-size:10px; min-width:140px; }

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
    border:1px solid rgba(74,222,128,0.25); background:rgba(74,222,128,0.06);
    font-size:11px; color:var(--green);
  }
  .update-version { font-size:12px; font-weight:600; color:var(--amber); margin-bottom:4px; }
  .update-notes { font-size:11px; color:var(--text-2); }
  .update-actions { display:flex; gap:8px; margin-top:16px; }
  .btn-accent {
    padding:8px 16px; border-radius:8px; border:none;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    color:#fff; font-size:11px; font-weight:600; cursor:pointer;
    font-family:inherit; transition:opacity .15s;
  }
  .btn-accent:hover { opacity:.88; }
  .btn-accent:disabled { opacity:.5; cursor:not-allowed; }
  .btn-secondary {
    padding:8px 16px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer;
    font-family:inherit; transition:all .15s;
  }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
  .btn-secondary:disabled { opacity:.5; cursor:not-allowed; }
  .update-msg { font-size:11px; margin-top:10px; color:var(--green); }
  .update-msg.error { color:var(--red); }
  .update-progress {
    display:flex; align-items:center; gap:10px;
    margin-top:12px; font-size:11px; color:var(--text-2);
  }
</style>
