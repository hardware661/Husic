import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api, refOf, type Item, type Playlist } from './api';
import { toast } from './toast';
import { localAudioID, setLocalAudioLiked, useLocalAudio } from './local-audio';

// Hearts and playlists, shared by every view so a heart toggled in one place
// shows everywhere.

type Library = {
  playlists: Playlist[];
  isLiked: (item: Item) => boolean;
  toggleLike: (item: Item) => Promise<void>;
  addTo: (playlistId: number, items: Item[]) => Promise<void>;
  createPlaylist: (name: string) => Promise<number>;
  renamePlaylist: (playlist: Playlist, name: string) => Promise<void>;
  refresh: () => void;
  version: number; // bumps when likes or playlists change
};

const LibraryContext = createContext<Library | null>(null);

export function useLibrary() {
  const library = useContext(LibraryContext);
  if (!library) throw new Error('useLibrary outside LibraryProvider');
  return library;
}

export function LibraryProvider({ children }: { children: ReactNode }) {
  const [liked, setLiked] = useState<Set<string>>(new Set());
  const [playlists, setPlaylists] = useState<Playlist[]>([]);
  const [version, setVersion] = useState(0);
  const { records: localRecords } = useLocalAudio();
  const localLikes = useMemo(() => new Map(localRecords.map((record) => [record.id, record.liked])), [localRecords]);
  const pendingLocalLikes = useRef(new Set<string>());

  const refresh = useCallback(() => {
    api
      .library()
      .then(({ likes, playlists }) => {
        setLiked(new Set(likes.flatMap((item) => [`song:${item.songId}`, ...item.copies.map(refOf)])));
        setPlaylists(playlists);
      })
      .catch(() => {});
  }, []);
  useEffect(refresh, [refresh]);

  const keys = (item: Item) => [...(item.songId ? [`song:${item.songId}`] : []), ...item.copies.map(refOf)];

  const isLiked = useCallback((item: Item) => {
    const localID = localAudioID(item);
    return localID ? localLikes.get(localID) ?? item.liked : keys(item).some((k) => liked.has(k));
  }, [liked, localLikes]);

  const toggleLike = useCallback(
    async (item: Item) => {
      const localID = localAudioID(item);
      if (localID) {
        if (pendingLocalLikes.current.has(localID)) return;
        pendingLocalLikes.current.add(localID);
        try {
          await setLocalAudioLiked(localID, !(localLikes.get(localID) ?? item.liked));
        } catch {
          toast('没能保存，稍后再试');
        } finally {
          pendingLocalLikes.current.delete(localID);
        }
        return;
      }
      const next = !keys(item).some((k) => liked.has(k));
      const apply = (on: boolean, extra: string[] = []) =>
        setLiked((set) => {
          const copy = new Set(set);
          for (const k of [...keys(item), ...extra]) on ? copy.add(k) : copy.delete(k);
          return copy;
        });
      apply(next);
      try {
        const { songId } = await api.like(item, next);
        apply(next, [`song:${songId}`]);
        setVersion((v) => v + 1);
      } catch {
        apply(!next);
        toast('没能保存，稍后再试');
      }
    },
    [liked, localLikes],
  );

  const addTo = useCallback(async (playlistId: number, items: Item[]) => {
    if (items.some((item) => localAudioID(item))) {
      toast('本地音乐不能加入在线歌单');
      return;
    }
    try {
      await api.addToPlaylist(playlistId, items);
      toast('已加入歌单');
      setVersion((v) => v + 1);
      refresh();
    } catch {
      toast('没能加入歌单');
    }
  }, [refresh]);

  const createPlaylist = useCallback(async (name: string) => {
    const { id } = await api.createPlaylist(name);
    refresh();
    setVersion((v) => v + 1);
    return id;
  }, [refresh]);

  const renamePlaylist = useCallback(async (playlist: Playlist, name: string) => {
    await api.renamePlaylist(playlist.id, name, playlist.description, playlist.cover);
    setPlaylists(current => current.map(p => p.id === playlist.id ? { ...p, name } : p));
    setVersion(v => v + 1);
    refresh();
  }, [refresh]);

  const value = useMemo(
    () => ({ playlists, isLiked, toggleLike, addTo, createPlaylist, renamePlaylist, refresh, version }),
    [playlists, isLiked, toggleLike, addTo, createPlaylist, renamePlaylist, refresh, version],
  );
  return <LibraryContext.Provider value={value}>{children}</LibraryContext.Provider>;
}
