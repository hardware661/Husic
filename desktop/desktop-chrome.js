(() => {
  'use strict';
  const musicOrigin = __HUSIC_ORIGIN__;
  const loginOrigin = __HUSIC_LOGIN_ORIGIN__;
  if (window !== window.top || !window.chrome?.webview || ![musicOrigin, loginOrigin].filter(Boolean).includes(location.origin)) return;
  const bridge = window.chrome.webview;
  const isMusic = location.origin === musicOrigin;
  const css = __HUSIC_DESKTOP_CSS__;
  const icons = {
    minimize: '<path d="M3 9.5h10"/>',
    maximize: '<rect x="3.5" y="3.5" width="9" height="9" rx=".6"/>',
    restore: '<path d="M5.5 3.5v-1h8v8h-1"/><rect x="2.5" y="5.5" width="8" height="8" rx=".6"/>',
    close: '<path d="m3.5 3.5 9 9m0-9-9 9"/>',
    window: '<rect x="2.5" y="3.5" width="15" height="13" rx="2"/><path d="M2.5 7.5h15M6 5.5h.01"/>',
    pin: '<path d="m7 3 6 0-1 5 3 3H5l3-3-1-5ZM10 11v6"/>',
    size: '<path d="M3 15 7 5l4 10M4.3 12h5.4m4.3-4 3-3m0 0h-3m3 0v3"/>',
    lyrics: '<rect x="2" y="4" width="16" height="12" rx="2"/><path d="M6 8h8M6 12h5"/>',
  };
  const svg = (name, size = 16) => `<svg width="${size}" height="${size}" viewBox="0 0 ${size === 16 ? 16 : 20} ${size === 16 ? 16 : 20}" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name]}</svg>`;
  let state = { closeToTray: true, topmost: false, zoom: 1, maximized: false, version: '0.1.0', lyricsMinFont:18, lyricsMaxFont:42, lyricsSize:28, lyricsColor:'#ffffff', lyricsSecondaryColor:'#d8d8e2', lyricsBackdrop:0, lyricsOrientation:'horizontal', lyricsAutoDock:false, lyricsSecondaryEnabled:true, windowScale:1, customWindowSize:false };
  let controls, hitRegions, section, lastDialog, scheduled = false, goToSettings = false;
  const send = (type, detail = {}) => bridge.postMessage({ type, ...detail });

  function renderState() {
    if (controls) {
      const maximize = controls.querySelector('[data-command="maximize"]');
      const label = state.maximized ? '还原窗口' : '最大化';
      maximize.setAttribute('aria-label', label);
      maximize.dataset.label = label;
      maximize.innerHTML = svg(state.maximized ? 'restore' : 'maximize');
      controls.style.zoom = String(1 / state.zoom);
    }
    if (hitRegions) {
      hitRegions.style.zoom = String(1 / state.zoom);
      hitRegions.dataset.maximized = String(state.maximized);
    }
    if (!section?.isConnected) return;
    for (const input of section.querySelectorAll('input[name="husic-desktop-close"]')) {
      input.checked = (input.value === 'tray') === state.closeToTray;
      input.closest('label').classList.toggle('is-selected', input.checked);
    }
    section.querySelector('[data-setting="topmost"]').setAttribute('aria-checked', String(state.topmost));
    section.querySelector('[data-setting="lyricsEnabled"]').setAttribute('aria-checked', String(state.lyricsEnabled === true));
    const lyricOptions=section.querySelector('.desktop-lyrics-disclosure');
    lyricOptions.classList.toggle('is-open',state.lyricsEnabled===true);
    lyricOptions.inert=state.lyricsEnabled!==true;
    lyricOptions.setAttribute('aria-hidden',String(state.lyricsEnabled!==true));
    section.querySelector('[data-setting="lyricsSecondaryEnabled"]').setAttribute('aria-checked',String(state.lyricsSecondaryEnabled!==false));
    const lock = section.querySelector('[data-setting="lyricsLocked"]');
    lock.textContent = state.lyricsLocked ? '解锁歌词' : '锁定歌词';
    lock.disabled = !state.lyricsEnabled && !state.lyricsLocked;
    section.querySelector('[data-lyric-size="less"]').disabled = state.lyricsSize <= state.lyricsMinFont;
    section.querySelector('[data-lyric-size="more"]').disabled = state.lyricsSize >= state.lyricsMaxFont;
    section.querySelector('[data-lyric-size="reset"]').textContent = `${state.lyricsSize || 28}`;
    for (const input of section.querySelectorAll('[data-lyric-pref]')) {
      const key=input.dataset.lyricPref;
      if (input.type==='radio') {
        input.checked=state[key]===input.value;
        input.closest('label').classList.toggle('is-selected',input.checked);
      }
      else if (document.activeElement!==input) input.value=String(state[key]);
      if(input.type==='range')input.style.setProperty('--fill',`${Number(state[key])/Number(input.max)*100}%`);
    }
    section.querySelector('[data-lyric-pref="lyricsMinFont"]').max=String(state.lyricsMaxFont);
    section.querySelector('[data-lyric-pref="lyricsMaxFont"]').min=String(state.lyricsMinFont);
    section.querySelector('[data-lyric-opacity]').textContent=state.lyricsBackdrop===0?'透明':`${state.lyricsBackdrop}%`;
    for (const swatch of section.querySelectorAll('[data-lyric-color]')) swatch.setAttribute('aria-pressed',String(swatch.dataset.lyricColor===state.lyricsColor));
    for (const swatch of section.querySelectorAll('[data-lyric-secondary-color]')) swatch.setAttribute('aria-pressed',String(swatch.dataset.lyricSecondaryColor===state.lyricsSecondaryColor));
    section.querySelector('[data-zoom="reset"]').textContent = `${Math.round(state.zoom * 100)}%`;
    section.querySelector('[data-zoom="less"]').disabled = state.zoom <= .85;
    section.querySelector('[data-zoom="more"]').disabled = state.zoom >= 1.25;
    section.querySelector('[data-window-size="reset"]').textContent=state.customWindowSize?'自定':`${Math.round((state.windowScale||1)*100)}%`;
    section.querySelector('[data-window-size="less"]').disabled=state.windowScale<=.7;
    section.querySelector('[data-window-size="more"]').disabled=state.windowScale>=1.25;
    section.querySelector('.desktop-settings-error').textContent = state.error || '';
    section.querySelector('.desktop-window-notice').textContent=state.notice||'';
  }

  function installSettings() {
    if (!isMusic) return;
    const page = document.querySelector('.settings-page');
    if (!page || page.querySelector('#husic-desktop-settings')) return;
    section = document.createElement('section');
    section.id = 'husic-desktop-settings';
    section.className = 'settings-section desktop-settings';
    section.setAttribute('aria-labelledby', 'husic-desktop-heading');
    section.innerHTML = `
      <h2 id="husic-desktop-heading">桌面体验</h2>
      <div class="settings-group">
        <div class="setting-row">
          <span class="setting-symbol">${svg('lyrics', 20)}</span>
          <div class="setting-copy"><strong>桌面歌词</strong></div>
          <div class="setting-control"><button class="desktop-switch" type="button" role="switch" aria-label="桌面歌词" aria-checked="false" data-setting="lyricsEnabled"><span></span></button></div>
        </div>
        <div class="desktop-lyrics-disclosure" inert aria-hidden="true"><div class="desktop-lyrics-options">
        <div class="setting-row">
          <span class="setting-symbol">${svg('lyrics', 20)}</span>
          <div class="setting-copy"><strong>歌词布局</strong></div>
          <div class="setting-control"><fieldset class="desktop-close-choice" aria-label="歌词方向">
            <label><input type="radio" name="husic-lyric-direction" data-lyric-pref="lyricsOrientation" value="horizontal"><span>横向</span></label>
            <label><input type="radio" name="husic-lyric-direction" data-lyric-pref="lyricsOrientation" value="vertical"><span>竖向</span></label>
          </fieldset><button class="pill" type="button" data-setting="lyricsLocked">锁定歌词</button></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('lyrics',20)}</span><div class="setting-copy"><strong>副歌词</strong></div>
          <div class="setting-control"><button class="desktop-switch" type="button" role="switch" aria-label="副歌词" aria-checked="true" data-setting="lyricsSecondaryEnabled"><span></span></button></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('size',20)}</span><div class="setting-copy"><strong>歌词字号</strong></div>
          <div class="setting-control"><div class="desktop-size-control" role="group" aria-label="歌词字号"><button type="button" aria-label="缩小歌词" data-lyric-size="less">−</button><button type="button" aria-label="恢复适中歌词" data-lyric-size="reset">28</button><button type="button" aria-label="放大歌词" data-lyric-size="more">+</button></div></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('size',20)}</span><div class="setting-copy"><strong>字号范围</strong></div>
          <div class="setting-control"><div class="desktop-font-range" role="group" aria-label="字号范围"><label><span>最小</span><input type="number" min="12" max="72" step="1" aria-label="歌词最小字号" data-lyric-pref="lyricsMinFont"></label><i>—</i><label><span>最大</span><input type="number" min="18" max="96" step="1" aria-label="歌词最大字号" data-lyric-pref="lyricsMaxFont"></label></div></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('lyrics',20)}</span><div class="setting-copy"><strong>主歌词颜色</strong></div>
          <div class="setting-control"><div class="desktop-lyric-swatches" role="group" aria-label="歌词色系">${[['#ffffff','月白'],['#f4cf7b','流金'],['#f5a1bc','浅玫'],['#c4aaff','雾紫'],['#9dd8b6','薄荷'],['#9bd8f1','晴空']].map(([color,name])=>`<button type="button" aria-label="${name}歌词" data-lyric-color="${color}" aria-pressed="false" style="--swatch:${color}"></button>`).join('')}</div><input class="desktop-color-control" type="color" aria-label="自定义主歌词颜色" data-lyric-pref="lyricsColor"></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('lyrics',20)}</span><div class="setting-copy"><strong>副歌词颜色</strong></div>
          <div class="setting-control"><div class="desktop-lyric-swatches" role="group" aria-label="副歌词色系">${[['#ffffff','月白'],['#f4cf7b','流金'],['#f5a1bc','浅玫'],['#c4aaff','雾紫'],['#9dd8b6','薄荷'],['#9bd8f1','晴空']].map(([color,name])=>`<button type="button" aria-label="${name}副歌词" data-lyric-secondary-color="${color}" aria-pressed="false" style="--swatch:${color}"></button>`).join('')}</div><input class="desktop-color-control" type="color" aria-label="自定义副歌词颜色" data-lyric-pref="lyricsSecondaryColor"></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('window',20)}</span><div class="setting-copy"><strong>底板浓度</strong></div>
          <div class="setting-control"><div class="desktop-opacity-control"><input type="range" min="0" max="80" step="5" aria-label="歌词底板浓度" data-lyric-pref="lyricsBackdrop"><output data-lyric-opacity>透明</output></div></div>
        </div>
        </div></div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('window', 20)}</span>
          <div class="setting-copy"><strong>关闭窗口时</strong></div>
          <div class="setting-control"><fieldset class="desktop-close-choice" aria-label="关闭窗口时">
            <label><input type="radio" name="husic-desktop-close" value="tray"><span>收进托盘</span></label>
            <label><input type="radio" name="husic-desktop-close" value="exit"><span>退出 Husic</span></label>
          </fieldset></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('pin', 20)}</span>
          <div class="setting-copy"><strong>窗口置顶</strong></div>
          <div class="setting-control"><button class="desktop-switch" type="button" role="switch" aria-label="窗口置顶" aria-checked="false" data-setting="topmost"><span></span></button></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('size', 20)}</span>
          <div class="setting-copy"><strong>界面大小</strong></div>
          <div class="setting-control"><div class="desktop-size-control" role="group" aria-label="界面大小">
            <button type="button" aria-label="缩小界面" data-zoom="less"><svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M3 7h8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg></button>
            <button type="button" aria-label="恢复 100% 大小" data-zoom="reset">100%</button>
            <button type="button" aria-label="放大界面" data-zoom="more"><svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M3 7h8M7 3v8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg></button>
          </div></div>
        </div>
        <div class="setting-row">
          <span class="setting-symbol">${svg('window',20)}</span>
          <div class="setting-copy"><strong>窗口大小</strong></div>
          <div class="setting-control"><div class="desktop-size-control" role="group" aria-label="窗口大小"><button type="button" aria-label="缩小窗口" data-window-size="less">−</button><button type="button" aria-label="恢复默认窗口" data-window-size="reset">100%</button><button type="button" aria-label="放大窗口" data-window-size="more">+</button></div><button type="button" class="pill" data-save-window>保存当前窗口大小</button></div>
        </div>
      </div>
      <p class="desktop-window-notice" role="status" aria-live="polite"></p>
      <p class="desktop-settings-error" role="status" aria-live="polite"></p>`;
    section.addEventListener('input', event => {
      const input=event.target;
      if(event.isTrusted && input instanceof HTMLInputElement && input.dataset.lyricPref==='lyricsBackdrop') {
        input.style.setProperty('--fill',`${Number(input.value)/80*100}%`);section.querySelector('[data-lyric-opacity]').textContent=input.value==='0'?'透明':`${input.value}%`;
      }
    });
    section.addEventListener('change', event => {
      const input = event.target;
      if (event.isTrusted && input instanceof HTMLInputElement && input.dataset.lyricPref) {
        const key=input.dataset.lyricPref;
        send('desktop-settings-change',{name:key,value:['number','range'].includes(input.type)?Number(input.value):input.value});
      }
      if (event.isTrusted && input instanceof HTMLInputElement && input.name === 'husic-desktop-close') {
        send('desktop-settings-change', { name: 'closeToTray', value: input.value === 'tray' });
      }
    });
    section.addEventListener('click', event => {
      if (!event.isTrusted) return;
      const button = event.target instanceof Element ? event.target.closest('button') : null;
      if (!button || button.disabled) return;
      if (button.dataset.setting === 'topmost') send('desktop-settings-change', { name: 'topmost', value: !state.topmost });
      if (button.dataset.setting === 'lyricsEnabled') send('desktop-settings-change', { name: 'lyricsEnabled', value: !state.lyricsEnabled });
      if (button.dataset.setting === 'lyricsSecondaryEnabled') send('desktop-settings-change', { name: 'lyricsSecondaryEnabled', value: state.lyricsSecondaryEnabled===false });
      if (button.dataset.lyricColor) send('desktop-settings-change', { name: 'lyricsColor', value: button.dataset.lyricColor });
      if (button.dataset.lyricSecondaryColor) send('desktop-settings-change', { name: 'lyricsSecondaryColor', value: button.dataset.lyricSecondaryColor });
      if (button.dataset.setting === 'lyricsLocked') send('desktop-settings-change', { name: 'lyricsLocked', value: !state.lyricsLocked });
      if (button.dataset.lyricSize) {
        const preferred = button.dataset.lyricSize === 'reset' ? 28 : (state.lyricsSize || 28) + (button.dataset.lyricSize === 'more' ? 2 : -2);
        const next=Math.max(state.lyricsMinFont,Math.min(state.lyricsMaxFont,preferred));
        if (next !== undefined) send('desktop-settings-change', { name: 'lyricsSize', value: next });
      }
      if (button.hasAttribute('data-save-window')) send('desktop-settings-change',{name:'saveWindowSize',value:true});
      if (button.dataset.windowSize) {
        const next=button.dataset.windowSize==='reset'?1:Math.round(((state.windowScale||1)+(button.dataset.windowSize==='more'?.05:-.05))*100)/100;
        if(next>=.7 && next<=1.25)send('desktop-settings-change',{name:'windowScale',value:next});
      }
      if (button.dataset.zoom) {
        const levels = [.85, 1, 1.15, 1.25], current = levels.indexOf(state.zoom);
        const next = button.dataset.zoom === 'reset' ? 1 : levels[current + (button.dataset.zoom === 'more' ? 1 : -1)];
        if (next !== undefined) send('desktop-settings-change', { name: 'zoom', value: next });
      }
    });
    const signature = page.querySelector('.settings-signature');
    page.insertBefore(section, signature || null);
    if (signature && !signature.querySelector('.desktop-version')) {
      const version = document.createElement('span'); version.className = 'desktop-version'; version.textContent = 'Windows 0.1.0'; signature.append(version);
    }
    renderState();
    if (goToSettings) { goToSettings = false; section.scrollIntoView({ block: 'center', behavior: 'instant' }); }
  }

  function refreshLayers() {
    scheduled = false;
    installSettings();
    // Keep actual window controls above a web modal without weakening the
    // modal's focus handling. No second browser or native overlay is needed.
    const dialog = Array.from(document.querySelectorAll('dialog[open]')).pop() || null;
    if (dialog !== lastDialog && controls?.showPopover) {
      lastDialog = dialog;
      if (controls.matches(':popover-open')) controls.hidePopover();
      if (hitRegions.matches(':popover-open')) hitRegions.hidePopover();
      // A modal makes DOM siblings inert even when their popover is higher in
      // the top layer. Keep the controls inside the active modal's subtree.
      (dialog || document.body).append(hitRegions, controls);
      hitRegions.showPopover();
      controls.showPopover();
    }
  }

  function start() {
    if (!document.body || document.getElementById('husic-window-controls')) return;
    document.documentElement.dataset.husicDesktop = 'true';
    const style = document.createElement('style'); style.id = 'husic-desktop-style'; style.textContent = css; document.head.append(style);
    hitRegions = document.createElement('div'); hitRegions.className = 'husic-window-hit-regions'; hitRegions.setAttribute('aria-hidden', 'true');
    hitRegions.innerHTML = '<div class="husic-window-drag"></div>' + ['left', 'right', 'top', 'bottom', 'top-left', 'top-right', 'bottom-left', 'bottom-right'].map(edge => `<div class="husic-window-edge" data-edge="${edge}"></div>`).join('');
    document.body.append(hitRegions);
    controls = document.createElement('div'); controls.id = 'husic-window-controls'; controls.className = 'husic-window-controls'; controls.setAttribute('role', 'group'); controls.setAttribute('aria-label', '窗口操作');
    controls.innerHTML = ['minimize', 'maximize', 'close'].map((action, index) => {
      const label = ['最小化', '最大化', '关闭窗口'][index];
      return `<button type="button" data-command="${action}" aria-label="${label}" data-label="${label}">${svg(action)}</button>`;
    }).join('');
    controls.addEventListener('click', event => {
      const button = event.target instanceof Element ? event.target.closest('button[data-command]') : null;
      if (event.isTrusted && button) send('desktop-window-command', { action: button.dataset.command });
    });
    document.body.append(controls);
    if (controls.showPopover) {
      hitRegions.setAttribute('popover', 'manual'); hitRegions.showPopover();
      controls.setAttribute('popover', 'manual'); controls.showPopover();
    }
    document.addEventListener('pointerdown', event => {
      if (!event.isTrusted || event.button !== 0 || !event.isPrimary || !(event.target instanceof Element)) return;
      const target = event.target;
      const edge = target.closest('.husic-window-edge');
      const caption = target.closest('.husic-window-drag, .brand, .page-header');
      if (!edge && (!caption || target.closest('button, a, input, select, textarea, [role="button"], .page-actions'))) return;
      event.preventDefault();
      send('desktop-window-command', { action: edge ? `resize-${edge.dataset.edge}` : 'drag' });
    }, true);
    document.addEventListener('dblclick', event => {
      if (event.isTrusted && event.button === 0 && event.target instanceof Element && event.target.closest('.husic-window-drag, .brand, .page-header') && !event.target.closest('button, a, input, .page-actions')) {
        event.preventDefault(); send('desktop-window-command', { action: 'maximize' });
      }
    }, true);
    renderState();
    new MutationObserver(() => {
      if (!scheduled) { scheduled = true; queueMicrotask(refreshLayers); }
    }).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['open'] });
    refreshLayers();
    send('desktop-shell-ready');
  }

  bridge.addEventListener('message', event => {
    if (event.data?.type === 'husic-desktop-state') {
      const data = event.data;
      if (typeof data.closeToTray !== 'boolean' || typeof data.topmost !== 'boolean' || ![.85, 1, 1.15, 1.25].includes(data.zoom)) return;
      state = data; renderState();
    }
    if (event.data?.type === 'husic-desktop-open-settings' && isMusic) {
      goToSettings = true;
      const link = document.querySelector('.side-settings a[href="/settings"]');
      if (location.pathname !== '/settings' && link) link.click();
      else if (location.pathname !== '/settings') { history.pushState({}, '', '/settings'); dispatchEvent(new PopStateEvent('popstate')); }
      else { installSettings(); section?.scrollIntoView({ block: 'center', behavior: 'instant' }); goToSettings = false; }
    }
  });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, { once: true }); else start();
})();
