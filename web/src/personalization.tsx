import { useRef, useState, type CSSProperties } from 'react';
import { palettes, setPreference, usePreferences } from './preferences';
import { setBackground, useBackground } from './background';
import { toast } from './toast';

export function Personalization() {
  const preference = usePreferences(),
    background = useBackground();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const upload = async (file: File | null) => {
    setBusy(true);
    try {
      await setBackground(file);
      toast(file ? '背景已换好，只保存在这台设备' : '已恢复纯色背景');
    } catch (error) {
      toast(error instanceof Error ? error.message : '背景没有保存');
    } finally {
      setBusy(false);
      if (input.current) input.current.value = '';
    }
  };
  return (
    <>
      <div className="preference-panel color-personalization">
        <div className="preference-heading">
          <strong>色系</strong>
        </div>
        <fieldset className="palette-picker" aria-label="界面色系">
          <label className={`palette-option${!preference.accent ? ' is-selected' : ''}`}>
            <input type="radio" name="husic-palette" checked={!preference.accent} onChange={() => setPreference('accent', '')} />
            <span className="palette-dot neutral" />
            <span>原色</span>
          </label>
          {palettes.map((p) => (
            <label key={p.id} className={`palette-option${preference.accent === p.color ? ' is-selected' : ''}`}>
              <input
                type="radio"
                name="husic-palette"
                checked={preference.accent === p.color}
                onChange={() => setPreference('accent', p.color)}
              />
              <span className="palette-dot" style={{ background: p.color }} />
              <span>{p.name}</span>
            </label>
          ))}
        </fieldset>
        <label className="custom-color">
          <span>自定义颜色</span>
          <input
            type="color"
            aria-label="自定义界面颜色"
            value={preference.accent || '#45454d'}
            onChange={(event) => setPreference('accent', event.target.value)}
          />
          <code>{preference.accent || '默认'}</code>
        </label>
      </div>
      <div className="preference-panel wallpaper-personalization">
        <div className="preference-heading">
          <strong>背景图片</strong>
        </div>
        <div className="wallpaper-actions">
          <button className="pill" disabled={busy} onClick={() => input.current?.click()}>
            {busy ? '正在处理…' : background.url ? '更换图片' : '选择图片'}
          </button>
          {background.url && (
            <button className="text-button" disabled={busy} onClick={() => void upload(null)}>
              移除背景
            </button>
          )}
          <input
            ref={input}
            type="file"
            accept="image/jpeg,image/png,image/webp,image/avif"
            aria-label="上传背景图片"
            hidden
            disabled={busy}
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) void upload(file);
            }}
          />
        </div>
        <div className="wallpaper-sliders">
          <label>
            <span>
              浓度 <output>{preference.backgroundStrength}%</output>
            </span>
            <input
              type="range"
              aria-label="背景浓度"
              style={{ '--fill': `${preference.backgroundStrength * 2}%` } as CSSProperties}
              min={0}
              max={50}
              value={preference.backgroundStrength}
              onChange={(e) => setPreference('backgroundStrength', Number(e.target.value))}
            />
          </label>
          <label>
            <span>
              柔化 <output>{preference.backgroundBlur}</output>
            </span>
            <input
              type="range"
              aria-label="背景柔化"
              style={{ '--fill': `${preference.backgroundBlur * 5}%` } as CSSProperties}
              min={0}
              max={20}
              value={preference.backgroundBlur}
              onChange={(e) => setPreference('backgroundBlur', Number(e.target.value))}
            />
          </label>
        </div>
      </div>
    </>
  );
}
