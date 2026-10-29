import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, ApiError, type AccountStatus, type ConnectionAttempt, type QRKind } from './api';
import { PageHeader } from './components';
import { Icon } from './icons';
import { useLibrary } from './library';
import { setPreference, usePreferences } from './preferences';
import { AppearancePicker, MotionPicker } from './settings-controls';
import { Personalization } from './personalization';
import { toast } from './toast';
import { Link } from './router';
import { SleepTimerControl } from './sleep-timer-control';

const qrNames: Record<QRKind, string> = { netease: '网易云', qq: 'QQ 音乐', bilibili: 'B 站', kugou: '酷狗音乐' };
const qrApps: Record<QRKind, string> = { netease: '网易云音乐', qq: 'QQ', bilibili: '哔哩哔哩', kugou: '酷狗音乐' };
const isQR = (kind: string | null): kind is QRKind => kind === 'netease' || kind === 'qq' || kind === 'bilibili' || kind === 'kugou';

type BridgeReply = { ok: boolean; error?: string; attempt?: ConnectionAttempt };
function connector(action: 'ping' | 'start' | 'complete' | 'cancel', id = ''): Promise<BridgeReply> {
  return new Promise((resolve) => {
    const requestId = crypto.randomUUID();
    const finish = (reply: BridgeReply) => { clearTimeout(timer); window.removeEventListener('message', receive); resolve(reply); };
    const receive = (event: MessageEvent) => {
      if (event.source !== window || event.origin !== location.origin || event.data?.type !== 'MUSIC_CONNECT_RESPONSE' || event.data.requestId !== requestId) return;
      finish(event.data.result);
    };
    const timer = window.setTimeout(() => finish({ ok: false, error: 'extension_missing' }), action === 'complete' ? 45000 : action === 'ping' ? 1200 : 12000);
    window.addEventListener('message', receive);
    window.postMessage({ type: 'MUSIC_CONNECT_REQUEST', requestId, action, id }, location.origin);
  });
}

const statusText: Record<string, string> = {
  connected: '已连接', configured: '待验证', not_connected: '未连接', needs_login: '需要重新登录',
  verification_required: '需要验证', unavailable: '暂时无法连接',
};
const bridgeErrors: Record<string, string> = {
  extension_missing: '尚未找到 连接扩展，请先完成一次安装。',
  incognito_permission: '请在 连接扩展的详情中，开启“允许在无痕模式下运行”。',
  private_window_open: '请先关闭其它无痕窗口，再打开这次专用登录窗口。',
  login_window_closed: '登录窗口已关闭，请重新开始连接。',
  login_incomplete: '还没有收到 YouTube 登录，请在专用窗口中完成登录。',
  window_failed: '无法打开专用登录窗口，请重试。',
  transfer_failed: '连接没有完成，请确认 Husic 仍处于登录状态后重试。',
};

function Dialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => { ref.current?.showModal(); }, []);
  return <dialog className="connection-dialog" ref={ref} aria-label={title} onCancel={(event) => { event.preventDefault(); onClose(); }}>
    <header><h2>{title}</h2><button className="icon-button" aria-label="关闭" onClick={onClose}><Icon name="close" /></button></header>
    {children}
  </dialog>;
}

function SettingRow({ icon, title, detail, children, className = '' }: { icon: ReactNode; title: string; detail?: ReactNode; children: ReactNode; className?: string }) {
  return <div className={`setting-row ${className}`}><span className="setting-symbol">{icon}</span><div className="setting-copy"><strong>{title}</strong>{detail && <p>{detail}</p>}</div><div className="setting-control">{children}</div></div>;
}

export function Settings() {
  const preferences = usePreferences();
  const { refresh } = useLibrary();
  const [accounts, setAccounts] = useState<AccountStatus | null>(null);
  const [accountError, setAccountError] = useState(false);
  const [attempt, setAttempt] = useState<ConnectionAttempt | null>(null);
  const [dialog, setDialog] = useState<QRKind | 'youtube' | 'extension' | null>(null);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [syncing, setSyncing] = useState(false);
  const flow = useRef(0);
  const activeAttempt = useRef(attempt);
  activeAttempt.current = attempt;
  useEffect(() => () => {
    flow.current++;
    const active = activeAttempt.current;
    if (active) {
      void api.cancelConnection(active.id).catch(() => {});
      if (active.kind === 'youtube') void connector('cancel', active.id);
    }
  }, []);
  const refreshStatus = useCallback(() => {
    setAccountError(false);
    return api.accounts().then(setAccounts).catch(() => setAccountError(true));
  }, []);
  useEffect(() => { void refreshStatus(); }, [refreshStatus]);
  const close = () => {
    flow.current++;
    if (attempt) {
      void api.cancelConnection(attempt.id).catch(() => {});
      if (attempt.kind === 'youtube') void connector('cancel', attempt.id);
    }
    setDialog(null); setAttempt(null); setMessage(''); setBusy('');
  };
  const startQR = async (kind: QRKind) => {
    const sequence = ++flow.current;
    setBusy(kind); setMessage(''); setDialog(kind);
    if (attempt) void api.cancelConnection(attempt.id).catch(() => {});
    setAttempt(null);
    try { const next = await api.startConnection(kind); if (flow.current === sequence) setAttempt(next); else void api.cancelConnection(next.id).catch(() => {}); }
    catch (error) { if (flow.current === sequence) setMessage(error instanceof Error ? error.message : '二维码暂时无法生成，请重试'); }
    finally { if (flow.current === sequence) setBusy(''); }
  };
  const startNetEase = () => startQR('netease');
  useEffect(() => {
    if (!isQR(dialog) || !attempt || !['waiting', 'scanned'].includes(attempt.state)) return;
    let cancelled = false;
    let timer = 0;
    const poll = async () => {
      if (cancelled) return;
      if (Date.now() > Date.parse(attempt.expires)) { setAttempt({ ...attempt, state: 'expired' }); return; }
      try {
        const next = await api.checkConnection(dialog, attempt.id);
        if (cancelled) return;
        if (next.state === 'connected') {
          setAttempt(null); setDialog(null); refresh(); void refreshStatus(); toast(`${qrNames[dialog]}已连接`); return;
        }
        if (next.state !== attempt.state) { setAttempt(next); return; }
        setMessage('');
      } catch (error) {
        if (!cancelled) {
          if (error instanceof ApiError && error.status === 410) {setAttempt({ ...attempt, state: 'expired', message: error.message });return;}
          setMessage('连接暂时不通，稍后会继续检查');
        }
      }
      if (!cancelled) timer = window.setTimeout(poll, 3000);
    };
    timer = window.setTimeout(poll, 800);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [dialog, attempt, refresh, refreshStatus]);
  const startYouTube = async () => {
    const sequence = ++flow.current;
    setMessage('');
    if (navigator.maxTouchPoints > 0 && matchMedia('(pointer: coarse)').matches) { setDialog('youtube'); return; }
    setBusy('youtube');
    try {
      const found = await connector('ping');
      if (flow.current !== sequence) return;
      if (!found.ok) { setDialog('extension'); return; }
      const next = await api.startYouTube();
      if (flow.current !== sequence) { void api.cancelConnection(next.id).catch(() => {}); return; }
      setAttempt(next); setDialog('youtube');
      const reply = await connector('start', next.id);
      if (flow.current !== sequence) return;
      if (!reply.ok) { setMessage(bridgeErrors[reply.error || ''] || '连接暂时没有完成，请重试'); setAttempt({ ...next, state: 'failed' }); }
    } catch (error) { if (flow.current === sequence) { setMessage(error instanceof Error ? error.message : '暂时无法打开登录'); setDialog('youtube'); } }
    finally { if (flow.current === sequence) setBusy(''); }
  };
  const completeYouTube = async () => {
    if (!attempt) return;
    const sequence = flow.current;
    setBusy('verify'); setMessage('正在确认这份登录能否播放音乐…');
    const reply = await connector('complete', attempt.id);
    if (flow.current !== sequence) return;
    setBusy('');
    if (reply.ok && reply.attempt?.state === 'connected') {
      setAttempt(null); setDialog(null); void refreshStatus(); toast('YouTube 已连接，音频验证通过');
    } else {
      setMessage(reply.attempt?.message || bridgeErrors[reply.error || ''] || '连接未完成，原连接保持不变');
      setAttempt(reply.attempt || { ...attempt, state: 'failed' });
      void refreshStatus();
    }
  };
  const sync = async () => {
    setSyncing(true);
    try { const result = await api.syncNetEase(); refresh(); toast(`同步好了：${result.liked} 首喜欢，${result.playlists} 个歌单`); }
    catch { toast('同步没有完成，请先检查网易云连接'); }
    finally { setSyncing(false); }
  };
  return <div className="page settings-page">
    <PageHeader title="设置" search={false} />
    <section className="settings-section" aria-labelledby="settings-common"><h2 id="settings-common">常用</h2>
      <div className="settings-group"><SleepTimerControl /></div>
    </section>
    <section className="settings-section" aria-labelledby="settings-accounts"><h2 id="settings-accounts">账号与连接</h2>
      <div className="settings-group">
        <SettingRow icon={<Icon name="netease" />} title="网易云音乐" detail={accounts?.netease.name}>
          <span className={`connection-state${accounts?.netease.state === 'connected' ? ' is-connected' : ''}`}>{accountError ? '检查未完成' : accounts ? statusText[accounts.netease.state] || '待检查' : '正在检查…'}</span>
          <button className="pill" onClick={startNetEase} disabled={busy !== ''}>{accounts?.netease.state === 'connected' ? '重新连接' : '扫码登录'}</button>
        </SettingRow>
        <SettingRow className="setting-account-row" icon={<Icon name="bilibili" />} title="B 站" detail={accounts?.bilibili?.name}>
          <span className={`connection-state${accounts?.bilibili?.state === 'connected' ? ' is-connected' : ''}`}>{accountError ? '检查未完成' : accounts ? statusText[accounts.bilibili?.state || 'not_connected'] || '待检查' : '正在检查…'}</span>
          <button className="pill" onClick={() => startQR('bilibili')} disabled={busy !== ''}>{accounts?.bilibili?.state === 'connected' ? '重新连接' : '扫码登录'}</button>
        </SettingRow>
        <SettingRow icon={<Icon name="youtube" />} title="YouTube Music">
          <span className={`connection-state${accounts?.youtube.state === 'connected' ? ' is-connected' : ''}`}>{accountError ? '检查未完成' : accounts ? statusText[accounts.youtube.state] || '待检查' : '正在检查…'}</span>
          <button className="pill" onClick={startYouTube} disabled={busy !== ''}>重新连接</button>
        </SettingRow>
      </div>
      <div className="settings-footnote">{accountError && <button className="text-button" onClick={refreshStatus}>重新获取状态</button>}</div>
    </section>
    <section className="settings-section" aria-labelledby="settings-library"><h2 id="settings-library">音乐库</h2><div className="settings-group">
      <SettingRow icon={<Icon name="sync" />} title="同步网易云"><button className="pill" onClick={sync} disabled={syncing}>{syncing ? '正在同步…' : '立即同步'}</button></SettingRow>
      <SettingRow icon={<Icon name="bilibili" />} title="B 站收藏与链接"><Link className="pill" to="/account/bilibili">打开收藏</Link></SettingRow>
    </div></section>
    <section className="settings-section" aria-labelledby="settings-appearance"><h2 id="settings-appearance">外观与动效</h2><div className="settings-group">
      <div className="preference-panel"><div className="preference-heading"><strong>外观</strong></div><AppearancePicker value={preferences.theme} onChange={(value) => setPreference('theme', value)} /></div>
      <Personalization />
      <div className="preference-panel"><div className="preference-heading"><strong>动态效果</strong></div><MotionPicker value={preferences.motion} onChange={(value) => setPreference('motion', value)} /></div>
    </div></section>
    <p className="settings-signature">Husic <span>·</span> hardware</p>
    {isQR(dialog) && <Dialog title={`连接${dialog === 'netease' ? '' : ' '}${qrNames[dialog]}`} onClose={close}>
      <div className="qr-stage" aria-live="polite">{busy === dialog ? <span>正在准备二维码…</span> : attempt?.kind === dialog && ['waiting', 'scanned'].includes(attempt.state) ? <img width={256} height={256} src={`/api/accounts/${dialog}/login/${encodeURIComponent(attempt.id)}/qr`} alt={`${qrNames[dialog]}登录二维码`} /> : <span>{attempt?.state === 'expired' ? '二维码已过期' : attempt?.message || '二维码尚未准备好'}</span>}</div>
      <p className="connection-progress" role="status">{attempt?.state === 'scanned' ? `已扫码，请在${qrApps[dialog]}中确认` : message || attempt?.message || (attempt?.state === 'expired' ? '这次二维码已结束，请重新开始' : '等待扫码确认')}</p>
      <div className="connection-actions"><button className="pill" onClick={() => startQR(dialog)} disabled={busy !== ''}>换一个二维码</button>{attempt && <a className="pill" href={`/api/accounts/${dialog}/login/${encodeURIComponent(attempt.id)}/qr`} download={`Husic-${qrNames[dialog]}登录.png`}>保存二维码</a>}</div>
    </Dialog>}
    {dialog === 'youtube' && <Dialog title="连接 YouTube Music" onClose={close}>
      <p className="connection-progress" role="status">{message}</p>
      {attempt && <div className="connection-actions"><button className="pill primary" onClick={completeYouTube} disabled={busy !== '' || attempt.state !== 'waiting'}>{busy === 'verify' ? '正在验证…' : '已完成登录，接回 Husic'}</button>{attempt.state !== 'waiting' && <button className="pill" onClick={startYouTube}>重新开始</button>}</div>}
    </Dialog>}
    {dialog === 'extension' && <Dialog title="首次连接准备" onClose={close}>
      <div className="connection-actions"><a className="pill" href="/connect/Music-Connect.zip" download>下载扩展</a><button className="pill primary" onClick={startYouTube} disabled={busy !== ''}>已安装，继续连接</button></div>
    </Dialog>}
  </div>;
}
