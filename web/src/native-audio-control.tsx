import { usePlayer } from './player';
import { useSleepDeadline } from './sleep-timer';
import { toast } from './toast';

const handoffKey = 'husic-native-audio-handoff-v1';

/** A diagnostic control, not a second player running beside the application.
 * Full-document navigation unloads React, CSS effects and Media Session hooks.
 * The exact resolved address is transferred once, never put in the URL/report. */
export function NativeAudioControl() {
  const { audio, stream, loading, pause } = usePlayer();
  const deadline = useSleepDeadline();
  const supported = Boolean(stream && /^https?:/.test(audio.src));
  const disabled = !supported || loading || deadline !== null;
  const start = () => {
    if (disabled || !stream) return;
    try {
      // Write first: a storage failure must not interrupt the current song.
      sessionStorage.setItem(handoffKey, JSON.stringify({
        url: audio.src, expires: Date.now() + 5 * 60_000,
        position: audio.currentTime, volume: audio.volume, muted: audio.muted,
        format: stream.format, bitrate: stream.bitrate, source: stream.source,
      }));
      // Use the player's pause action as well as the native pause so no pending
      // automatic advance can start the old element during this navigation.
      pause();
      // Safari can retain the old document in its back/forward cache. Release
      // its buffer and network request too, not only the audible playback.
      audio.removeAttribute('src');
      audio.load();
      location.assign('/native-audio.html');
    } catch {
      try { sessionStorage.removeItem(handoffKey); } catch { /* Storage can be unavailable. */ }
      toast('暂时无法打开对照播放，请留在 Husic 播放');
    }
  };
  return <>
    <p className="settings-signature"><button className="text-button" disabled={disabled} onClick={start}>原生播放对照</button></p>
    <p className="settings-signature">{deadline !== null ? '定时停止已开启，暂不切换播放方式' : supported ? '同一首歌、原音质，只播放当前歌曲' : '先播放一首在线歌曲，即可进行同曲对照'}</p>
  </>;
}
