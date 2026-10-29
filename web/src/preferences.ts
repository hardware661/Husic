import { useSyncExternalStore } from 'react';

export const palettes = [
  { id: 'graphite', name: '石墨', color: '#45454d' },
  { id: 'sage', name: '鼠尾草', color: '#517b68' },
  { id: 'iris', name: '鸢尾', color: '#8063a7' },
  { id: 'rose', name: '蔷薇', color: '#b55d78' },
  { id: 'amber', name: '琥珀', color: '#9c733b' },
  { id: 'ocean', name: '海盐', color: '#527e9b' },
] as const;
export type Preferences = { theme: 'system' | 'light' | 'dark'; motion: 'system' | 'reduced'; accent: string; backgroundStrength: number; backgroundBlur: number };
const key = 'husic-preferences-v1';
const listeners = new Set<() => void>();
let value: Preferences = { theme: 'system', motion: 'system', accent: '', backgroundStrength: 20, backgroundBlur: 0 };
export const validColor = (v: unknown): v is string => typeof v === 'string' && /^#[0-9a-f]{6}$/i.test(v);
try {
  const saved = JSON.parse(localStorage.getItem(key) || localStorage.getItem('music-preferences-v1') || '{}');
  if (['system', 'light', 'dark'].includes(saved.theme)) value.theme = saved.theme;
  if (['system', 'reduced'].includes(saved.motion)) value.motion = saved.motion;
  if (validColor(saved.accent)) value.accent = saved.accent;
  if (Number.isFinite(saved.backgroundStrength)) value.backgroundStrength = Math.max(0, Math.min(50, saved.backgroundStrength));
  if (Number.isFinite(saved.backgroundBlur)) value.backgroundBlur = Math.max(0, Math.min(20, saved.backgroundBlur));
} catch { /* A restricted browser still uses system defaults. */ }

const dark = matchMedia('(prefers-color-scheme: dark)');
const reduced = matchMedia('(prefers-reduced-motion: reduce)');
export const reduceMotion = () => value.motion === 'reduced' || reduced.matches;
function apply() {
  const root = document.documentElement;
  root.dataset.theme = value.theme === 'system' ? (dark.matches ? 'dark' : 'light') : value.theme;
  root.dataset.motion = reduceMotion() ? 'reduced' : 'standard';
  root.dataset.accent = value.accent ? 'custom' : 'neutral';
  root.style.setProperty('--accent', value.accent || (root.dataset.theme === 'dark' ? '#f5f5f7' : '#1d1d1f'));
  const rgb = (value.accent || '#45454d').slice(1).match(/../g)!.map((s) => parseInt(s, 16));
  const luminance = rgb.map((v) => { const n=v/255; return n<=.04045?n/12.92:((n+.055)/1.055)**2.4; }).reduce((sum,n,i) => sum+n*[.2126,.7152,.0722][i],0);
  root.style.setProperty('--accent-ink', luminance > .179 ? '#171719' : '#ffffff');
  const tinted=['--canvas','--sidebar','--header','--chrome','--fill','--fill-strong','--line','--selected','--accent-text'];
  if(value.accent){
    const base=root.dataset.theme==='dark'?'#19191c':'#ffffff';
    root.style.setProperty('--canvas',`color-mix(in srgb, ${value.accent} ${root.dataset.theme==='dark'?12:7}%, ${base})`);
    root.style.setProperty('--sidebar',`color-mix(in srgb, ${value.accent} 15%, ${base})`);
    root.style.setProperty('--header','color-mix(in srgb,var(--canvas) 86%,transparent)');
    root.style.setProperty('--chrome','color-mix(in srgb,var(--canvas) 88%,transparent)');
    root.style.setProperty('--fill',`color-mix(in srgb,${value.accent} 8%,transparent)`);
    root.style.setProperty('--fill-strong',`color-mix(in srgb,${value.accent} 13%,transparent)`);
    root.style.setProperty('--line',`color-mix(in srgb,${value.accent} 20%,transparent)`);
    root.style.setProperty('--selected',`color-mix(in srgb,${value.accent} 20%,transparent)`);
    root.style.setProperty('--accent-text',`color-mix(in srgb,${value.accent} 48%,var(--ink))`);
  } else tinted.forEach(name=>root.style.removeProperty(name));
  root.style.setProperty('--background-strength', String(value.backgroundStrength / 100));
  root.style.setProperty('--background-blur', `${value.backgroundBlur}px`);
  window.dispatchEvent(new Event('husic-preferences-change'));
}
export function setPreference<K extends keyof Preferences>(name: K, next: Preferences[K]) {
  if (name === 'accent' && next !== '' && !validColor(next)) return;
  if ((name === 'backgroundStrength' || name === 'backgroundBlur') && (typeof next !== 'number' || !Number.isFinite(next) || next < 0 || next > (name === 'backgroundStrength' ? 50 : 20))) return;
  if (value[name] === next) return;
  value = { ...value, [name]: next };
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* Settings still work for this visit. */ }
  const update = () => { apply(); listeners.forEach((listener) => listener()); };
  if (name === 'theme' && !reduceMotion() && document.startViewTransition) document.startViewTransition(update).ready.catch(() => {});
  else update();
}
export function usePreferences() {
  return useSyncExternalStore((listener) => { listeners.add(listener); return () => listeners.delete(listener); }, () => value);
}
dark.addEventListener('change', apply);
reduced.addEventListener('change', apply);
apply();
