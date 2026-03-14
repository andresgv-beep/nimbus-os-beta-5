import { writable, derived } from 'svelte/store';
import { getToken } from './auth.js';

const THEMES = ['dark', 'light', 'midnight'];

const ACCENT_COLORS = {
  orange: '#E95420', blue: '#42A5F5', green: '#66BB6A', purple: '#AB47BC',
  red: '#EF5350', amber: '#FFA726', cyan: '#26C6DA', pink: '#EC407A',
};

const DEFAULTS = {
  theme: 'dark', accentColor: 'orange', customAccentColor: '#E95420',
  glowIntensity: 50, taskbarSize: 'medium', taskbarPosition: 'bottom',
  autoHideTaskbar: false, clock24: true, showDesktopIcons: true,
  textScale: 100, wallpaper: '', showWidgets: true, widgetMode: 'dynamic',
  widgetScale: 100, pinnedApps: ['files', 'appstore', 'nimsettings'],
  widgetLayout: null,
};

// Single prefs store
export const prefs = writable({ ...DEFAULTS });

// Derived helpers
export const theme = derived(prefs, $p => $p.theme);
export const accentColor = derived(prefs, $p => ACCENT_COLORS[$p.accentColor] || $p.customAccentColor || ACCENT_COLORS.orange);
export const pinnedApps = derived(prefs, $p => $p.pinnedApps);

let saveTimeout = null;

// Apply theme to DOM
function applyToDOM(p) {
  const root = document.documentElement;
  root.setAttribute('data-theme', p.theme);

  const accent = ACCENT_COLORS[p.accentColor] || p.customAccentColor || ACCENT_COLORS.orange;
  root.style.setProperty('--accent', accent);

  const tbH = p.taskbarSize === 'small' ? 40 : p.taskbarSize === 'large' ? 56 : 48;
  root.style.setProperty('--taskbar-height', tbH + 'px');
  root.setAttribute('data-taskbar-pos', p.taskbarPosition);

  root.style.setProperty('--text-scale', (p.textScale / 100).toString());
  root.style.setProperty('--glow-intensity', (p.glowIntensity / 100).toString());
}

// Load from server
export async function loadPrefs() {
  // First: apply from localStorage (instant)
  try {
    const cached = localStorage.getItem('nimos-prefs');
    if (cached) {
      const p = { ...DEFAULTS, ...JSON.parse(cached) };
      prefs.set(p);
      applyToDOM(p);
    }
  } catch {}

  // Then: sync from server
  const token = getToken();
  if (!token) return;

  try {
    const res = await fetch('/api/user/preferences', {
      headers: { 'Authorization': `Bearer ${token}` },
    });
    const data = await res.json();
    if (data.preferences) {
      const p = { ...DEFAULTS, ...data.preferences };
      prefs.set(p);
      applyToDOM(p);
      localStorage.setItem('nimos-prefs', JSON.stringify(p));
    }
  } catch (err) {
    console.error('[Prefs] Load failed:', err.message);
  }
}

// Update a preference
export function setPref(key, value) {
  prefs.update(p => {
    const updated = { ...p, [key]: value };
    applyToDOM(updated);
    localStorage.setItem('nimos-prefs', JSON.stringify(updated));
    // Debounced save to server
    if (saveTimeout) clearTimeout(saveTimeout);
    saveTimeout = setTimeout(() => saveToServer(key, value), 1500);
    return updated;
  });
}

// Bulk update
export function setPrefs(updates) {
  prefs.update(p => {
    const updated = { ...p, ...updates };
    applyToDOM(updated);
    localStorage.setItem('nimos-prefs', JSON.stringify(updated));
    if (saveTimeout) clearTimeout(saveTimeout);
    saveTimeout = setTimeout(() => saveToServer(null, null, updates), 1500);
    return updated;
  });
}

async function saveToServer(key, value, bulk = null) {
  const token = getToken();
  if (!token) return;
  try {
    const body = bulk || { [key]: value };
    await fetch('/api/user/preferences', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
      body: JSON.stringify(body),
    });
  } catch {}
}

export { THEMES, ACCENT_COLORS, DEFAULTS };
