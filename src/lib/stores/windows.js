import { writable, derived } from 'svelte/store';

let nextZ = 100;
let counter = 0;
const posMap = {}; // { [id]: { x, y, width, height } } — hot state, no reactivity needed

export const windows = writable({});

export const windowList = derived(windows, $w => Object.values($w));

export function openWindow(appId, options = {}, webAppData = null) {
  const id = `w${++counter}`;
  const { width: reqW = 800, height: reqH = 520 } = options;

  const tbPos = document.documentElement.getAttribute('data-taskbar-pos') || 'bottom';
  const tbH = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--taskbar-height')) || 48;
  const offsetLeft = tbPos === 'left' ? tbH : 0;
  const offsetTop = tbPos === 'top' ? tbH : 0;

  const availW = window.innerWidth - offsetLeft;
  const availH = window.innerHeight - (tbPos !== 'left' ? tbH : 0);
  const width = Math.min(reqW, availW - 40);
  const height = Math.min(reqH, availH - 40);

  const offset = (counter % 6) * 30;
  const x = Math.max(offsetLeft + 20, Math.min((window.innerWidth - width) / 2 + offset, window.innerWidth - width - 10));
  const y = Math.max(offsetTop + 20, Math.min((window.innerHeight - height) / 2 - 40 + offset, window.innerHeight - height - tbH - 10));
  const zIndex = nextZ++;

  posMap[id] = { x, y, width, height };

  windows.update(w => ({
    ...w,
    [id]: {
      id, appId, zIndex, minimized: false, maximized: false, prevRect: null,
      isWebApp: webAppData?.isWebApp || false,
      webAppPort: webAppData?.port || null,
      webAppName: webAppData?.appName || null,
    },
  }));

  return id;
}

export function closeWindow(id) {
  delete posMap[id];
  windows.update(w => {
    const next = { ...w };
    delete next[id];
    return next;
  });
}

export function focusWindow(id) {
  windows.update(w => ({
    ...w,
    [id]: { ...w[id], zIndex: nextZ++, minimized: false },
  }));
}

export function minimizeWindow(id) {
  windows.update(w => ({
    ...w,
    [id]: { ...w[id], minimized: true },
  }));
}

export function maximizeWindow(id) {
  windows.update(w => {
    const win = w[id];
    const pos = posMap[id];
    if (!win || !pos) return w;

    if (win.maximized && win.prevRect) {
      Object.assign(posMap[id], win.prevRect);
      return { ...w, [id]: { ...win, maximized: false, prevRect: null } };
    }

    if (win.maximized) {
      posMap[id] = { x: (window.innerWidth - 800) / 2, y: (window.innerHeight - 520) / 2, width: 800, height: 520 };
      return { ...w, [id]: { ...win, maximized: false, prevRect: null } };
    }

    const tbH = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--taskbar-height')) || 48;
    const tbPos = document.documentElement.getAttribute('data-taskbar-pos') || 'bottom';
    let mx = 0, my = 0, mw = window.innerWidth, mh = window.innerHeight;
    if (tbPos === 'bottom') mh -= tbH;
    else if (tbPos === 'top') { my = tbH; mh -= tbH; }
    else if (tbPos === 'left') { mx = tbH; mw -= tbH; }

    const prevRect = { ...pos };
    posMap[id] = { x: mx, y: my, width: mw, height: mh };
    return { ...w, [id]: { ...win, maximized: true, prevRect } };
  });
}

export function restoreWindow(id) {
  windows.update(w => ({
    ...w,
    [id]: { ...w[id], minimized: false, zIndex: nextZ++ },
  }));
}

// Hot updates — no reactivity, direct DOM manipulation during drag
export function updateWindowPos(id, updates) {
  if (posMap[id]) Object.assign(posMap[id], updates);
}

export function getWindowPos(id) {
  return posMap[id] || { x: 0, y: 0, width: 800, height: 520 };
}
