import { useEffect, useId, useRef } from 'react';
import { Icon } from './icons';
import { reduceMotion, type Preferences } from './preferences';

const themes = [
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
  { value: 'system', label: '跟随系统' },
] as const;

function WindowPreview({ theme }: { theme: Preferences['theme'] }) {
  const clip = useId();
  const window = (dark: boolean) => <>
    <rect x="1" y="1" width="126" height="86" rx="8" fill={dark ? '#242426' : '#fff'} />
    <path d="M9 1h110a8 8 0 0 1 8 8v8H1V9a8 8 0 0 1 8-8Z" fill={dark ? '#39393c' : '#e8e8ea'} />
    {[9, 16, 23].map((x) => <circle key={x} cx={x} cy="9" r="1.8" fill={dark ? '#77777c' : '#b8b8be'} />)}
    <path d="M1 17h27v70H9a8 8 0 0 1-8-8Z" fill={dark ? '#303033' : '#f0f0f2'} />
    <rect x="7" y="27" width="14" height="3" rx="1.5" fill={dark ? '#9a9aa0' : '#b7b7be'} />
    <rect x="7" y="36" width="10" height="2.5" rx="1.25" fill={dark ? '#66666d' : '#d2d2d7'} />
    <rect x="7" y="44" width="12" height="2.5" rx="1.25" fill={dark ? '#66666d' : '#d2d2d7'} />
    <rect x="36" y="26" width="27" height="27" rx="4" fill={dark ? '#6c6c72' : '#c8c8ce'} />
    <circle cx="49.5" cy="39.5" r="7" stroke={dark ? '#96969d' : '#e8e8ec'} fill="none" />
    <rect x="72" y="29" width="38" height="4" rx="2" fill={dark ? '#dadade' : '#55555b'} />
    <rect x="72" y="38" width="26" height="3" rx="1.5" fill={dark ? '#777780' : '#c2c2c8'} />
    <rect x="72" y="46" width="33" height="3" rx="1.5" fill={dark ? '#777780' : '#c2c2c8'} />
    <rect x="35" y="65" width="83" height="13" rx="4" fill={dark ? '#3c3c40' : '#eeeef1'} />
    <path d="m42 69 5 2.5-5 2.5Z" fill={dark ? '#d1d1d7' : '#797980'} />
    <rect x="53" y="70" width="42" height="2" rx="1" fill={dark ? '#717179' : '#c8c8cf'} />
  </>;
  return <svg className="appearance-window" viewBox="0 0 128 88" aria-hidden="true" focusable="false">
    <defs><clipPath id={clip}><rect x="64" width="64" height="88" /></clipPath></defs>
    {window(theme === 'dark')}
    {theme === 'system' && <g clipPath={`url(#${clip})`}>{window(true)}</g>}
  </svg>;
}

export function AppearancePicker({ value, onChange }: { value: Preferences['theme']; onChange: (value: Preferences['theme']) => void }) {
  return <fieldset className="appearance-choices" aria-label="外观">
    {themes.map((theme) => <label key={theme.value} className={`appearance-option${value === theme.value ? ' is-selected' : ''}`}>
      <input type="radio" name="husic-appearance" value={theme.value} checked={value === theme.value} onChange={() => onChange(theme.value)} />
      <WindowPreview theme={theme.value} />
      <span className="appearance-caption"><span>{theme.label}</span><span className="appearance-check"><Icon name="check" size={10} /></span></span>
    </label>)}
  </fieldset>;
}

export function MotionPicker({ value, onChange }: { value: Preferences['motion']; onChange: (value: Preferences['motion']) => void }) {
  const cover = useRef<HTMLSpanElement>(null);
  const lines = useRef<HTMLSpanElement>(null);
  const animations = useRef<Animation[]>([]);
  const keyboard = useRef(false);
  useEffect(() => {
    const stop = () => animations.current.forEach((animation) => animation.cancel());
    window.addEventListener('husic-preferences-change', stop);
    return () => { stop(); window.removeEventListener('husic-preferences-change', stop); };
  }, []);
  const preview = (next = value) => {
    animations.current.forEach((animation) => animation.cancel());
    const simple = next === 'reduced' || reduceMotion();
    const options = { duration: simple ? 180 : 650, easing: 'cubic-bezier(.22,1,.36,1)' };
    const artwork = cover.current?.animate(simple ? [{ opacity: .35 }, { opacity: 1 }] : [
      { transform: 'translateX(-5px) scale(.94)', opacity: .5 },
      { transform: 'translateX(7px) scale(1.035)', opacity: 1, offset: .6 },
      { transform: 'translateX(0) scale(1)', opacity: 1 },
    ], options);
    const text = lines.current?.animate(simple ? [{ opacity: .4 }, { opacity: 1 }] : [
      { transform: 'translateY(5px)', opacity: .3 }, { transform: 'translateY(0)', opacity: 1 },
    ], options);
    animations.current = [artwork, text].filter((animation): animation is Animation => !!animation);
  };
  const select = (next: Preferences['motion']) => { onChange(next); if (!keyboard.current) preview(next); };
  return <div className="motion-picker">
    <fieldset className="motion-choices" aria-label="动态效果" data-reduced={value === 'reduced'} onPointerDown={(event) => { keyboard.current = false; delete event.currentTarget.dataset.keyboard; }} onKeyDown={(event) => { keyboard.current = true; event.currentTarget.dataset.keyboard = 'true'; }}>
      <span className="motion-choice-highlight" aria-hidden="true" />
      {[{ value: 'system', label: '标准动效' }, { value: 'reduced', label: '减少动效' }].map((option) => <label key={option.value} className={value === option.value ? 'is-selected' : ''}>
        <input type="radio" name="husic-motion" value={option.value} checked={value === option.value} onChange={() => select(option.value as Preferences['motion'])} />
        <span>{option.label}</span>
      </label>)}
    </fieldset>
    <div className="motion-example">
      <span className="motion-example-art" ref={cover} aria-hidden="true"><i /><b /></span>
      <span className="motion-example-lines" ref={lines} aria-hidden="true"><i /><i /></span>
      <button className="motion-preview-button" type="button" onClick={() => preview()} aria-label="播放动效预览"><Icon name="play" size={11} /><span>预览</span></button>
    </div>
  </div>;
}
