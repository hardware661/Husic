import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { artistLine, clock, sized, sourceNames, type Chart, type Item } from './api';
import { cachedCharts, cachedRanking, chartKey, loadCharts, loadRanking, warmRankings, type RankingData } from './chart-cache';
import { Bars, Cover, Empty, Heart, PageHeader, SongMenu, sameItem } from './components';
import { usePlayer } from './player';
import { navigate, query, usePath } from './router';
import { usePageVisible } from './view-activity';

// Charts: pick a platform, then one of its rankings. Number one gets a large
// card, two and three a smaller pair beside it; the rest are a numbered list.

const platforms = ['netease', 'qq', 'kuwo', 'kugou'];
const notes: Record<string, string> = {
  热歌: '此刻最热',
  新歌: '新鲜上榜',
  飙升: '热度上升',
  原创: '原创声音',
  流行指数: '流行风向',
  TOP500: '人气精选',
};

function tabKeys(e: KeyboardEvent<HTMLDivElement>) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
  const tabs = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
  const at = tabs.indexOf(e.target as HTMLButtonElement);
  if (at < 0) return;
  e.preventDefault();
  const next =
    e.key === 'Home' ? 0 : e.key === 'End' ? tabs.length - 1 : (at + (e.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
  tabs[next].focus();
  tabs[next].click();
}

export function Charts() {
  const path = usePath();
  const pageVisible = usePageVisible();
  const [charts, setCharts] = useState<Chart[]>(cachedCharts);
  const [answer, setAnswer] = useState<RankingData | null>(null);
  const [errorKey, setErrorKey] = useState('');
  const [retry, setRetry] = useState(0);
  const editions = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let alive = true;
    loadCharts()
      .then((r) => alive && setCharts(r))
      .catch(() => alive && setErrorKey('catalog'));
    return () => {
      alive = false;
    };
  }, [retry]);
  const wanted = query(path, 'c');
  const selected = useMemo(() => charts.find((c) => `${c.source}:${c.id}` === wanted) ?? charts[0], [charts, wanted]);
  const key = selected ? chartKey(selected) : '';
  const data = selected ? (answer && chartKey(answer.chart) === key ? answer : cachedRanking(selected)) : null;
  const failed = Boolean(errorKey) && (errorKey === key || errorKey === 'catalog');
  useEffect(() => {
    if (!selected) return;
    let alive = true;
    setErrorKey('');
    loadRanking(selected)
      .then((d) => alive && setAnswer(d))
      .catch(() => alive && setErrorKey(key));
    return () => {
      alive = false;
    };
  }, [selected, key, retry]);
  const ready = Boolean(data);
  useEffect(() => {
    if (!ready || !pageVisible || !charts.length || !matchMedia('(min-width: 761px) and (hover: hover) and (pointer: fine)').matches)
      return;
    let active = true;
    void warmRankings(charts, () => active);
    return () => {
      active = false;
    };
  }, [ready, pageVisible, charts]);
  const choose = (c: Chart) => navigate(`/charts?c=${c.source}:${c.id}`, true);
  const ofPlatform = charts.filter((c) => c.source === selected?.source);
  useLayoutEffect(() => {
    const node = editions.current;
    if (!node) return;
    const position = () => {
      const tab = node.querySelector<HTMLButtonElement>('[aria-selected="true"]');
      if (!tab) return;
      node.style.setProperty('--edition-x', `${tab.offsetLeft}px`);
      node.style.setProperty('--edition-y', `${tab.offsetTop}px`);
      node.style.setProperty('--edition-w', `${tab.offsetWidth}px`);
      node.style.setProperty('--edition-h', `${tab.offsetHeight}px`);
      node.dataset.ready = 'true';
    };
    position();
    const observer = new ResizeObserver(position);
    observer.observe(node);
    return () => observer.disconnect();
  }, [key, charts]);
  return (
    <div className="page charts-page">
      <PageHeader title="排行榜" />
      <div className="chart-switcher">
        <div className="chart-platforms" role="tablist" aria-label="平台" onKeyDown={tabKeys}>
          {platforms.map((p) => (
            <button
              key={p}
              role="tab"
              aria-selected={selected?.source === p}
              aria-controls="ranking-panel"
              tabIndex={selected?.source === p ? 0 : -1}
              className={selected?.source === p ? 'is-on' : ''}
              onClick={() => {
                const first = charts.find((c) => c.source === p && c.short === selected?.short) ?? charts.find((c) => c.source === p);
                if (first) choose(first);
              }}
              onPointerEnter={(event) => {
                const first = charts.find((c) => c.source === p);
                if (event.pointerType === 'mouse' && first) void loadRanking(first).catch(() => {});
              }}
              onFocus={(event) => {
                const first = charts.find((c) => c.source === p);
                if (event.currentTarget.matches(':focus-visible') && first) void loadRanking(first).catch(() => {});
              }}
            >
              {sourceNames[p]}
            </button>
          ))}
        </div>
        <div
          className="chart-editions"
          ref={editions}
          role="tablist"
          aria-label="榜单"
          onKeyDown={(event) => {
            event.currentTarget.dataset.keyboard = 'true';
            tabKeys(event);
          }}
          onPointerDown={(event) => {
            event.currentTarget.dataset.keyboard = 'false';
          }}
        >
          <span className="chart-edition-indicator" aria-hidden="true" />
          {ofPlatform.map((c, i) => (
            <button
              key={c.id}
              id={`chart-tab-${c.source}-${c.id}`}
              role="tab"
              aria-label={c.short}
              aria-controls="ranking-panel"
              aria-selected={c === selected}
              tabIndex={c === selected ? 0 : -1}
              className={c === selected ? 'is-on' : ''}
              onClick={() => choose(c)}
              onPointerEnter={(event) => {
                if (event.pointerType === 'mouse') void loadRanking(c).catch(() => {});
              }}
              onFocus={(event) => {
                if (event.currentTarget.matches(':focus-visible')) void loadRanking(c).catch(() => {});
              }}
            >
              <span className="edition-number" aria-hidden="true">
                {String(i + 1).padStart(2, '0')}
              </span>
              <span>
                <strong>{c.short}</strong>
                <small>{notes[c.short] ?? '发现好歌'}</small>
              </span>
            </button>
          ))}
        </div>
      </div>
      <div
        id="ranking-panel"
        role="tabpanel"
        aria-labelledby={selected ? `chart-tab-${selected.source}-${selected.id}` : undefined}
        aria-busy={!data && !failed}
      >
        {failed && (
          <Empty title="这个榜单暂时拿不到" hint="换一个榜单，或者稍后再试。">
            <button className="pill" onClick={() => setRetry((n) => n + 1)}>
              重新试试
            </button>
          </Empty>
        )}
        {!failed && !data && (
          <div className="chart-skeleton" aria-label="正在加载榜单">
            <div className="sk-chart-head" />
            <div className="sk-chart-podium">
              <i />
              <i />
              <i />
            </div>
          </div>
        )}
        {data && (
          <Ranking chart={data.chart} songs={data.songs}>
            <Rail charts={ofPlatform.filter((c) => c !== selected)} onPick={choose} />
          </Ranking>
        )}
      </div>
    </div>
  );
}

/** The platform's other charts at a glance: top three of each, one tap away. */
function Rail({ charts, onPick }: { charts: Chart[]; onPick: (c: Chart) => void }) {
  const pageVisible = usePageVisible();
  const [wide, setWide] = useState(() => matchMedia('(min-width: 1101px)').matches);
  useEffect(() => {
    const media = matchMedia('(min-width: 1101px)');
    const update = () => setWide(media.matches);
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, []);
  const [tops, setTops] = useState<Record<string, Item[]>>({});
  const keys = charts.map((c) => `${c.source}:${c.id}`).join(',');
  useEffect(() => {
    if (!wide || !pageVisible) return;
    let alive = true;
    for (const c of charts) {
      loadRanking(c)
        .then((d) => alive && setTops((t) => ({ ...t, [`${c.source}:${c.id}`]: d.songs.slice(0, 3) })))
        .catch(() => {});
    }
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keys, wide, pageVisible]);
  if (charts.length === 0) return null;
  return (
    <aside className="chart-rail">
      <h3>同平台的其他榜</h3>
      {charts.map((c) => {
        const top = tops[`${c.source}:${c.id}`] ?? [];
        return (
          <button key={c.id} className="rail-card" onClick={() => onPick(c)}>
            <strong>{c.name}</strong>
            <ol>
              {top.map((s, i) => (
                <li key={i}>
                  <span className="rail-no">{i + 1}</span>
                  <Cover src={s.cover} size={34} />
                  <span className="song-text">
                    <b>{s.title}</b>
                    <small>{artistLine(s)}</small>
                  </span>
                </li>
              ))}
            </ol>
            <span className="rail-more">看完整榜单 →</span>
          </button>
        );
      })}
    </aside>
  );
}

function Ranking({ chart, songs, children }: { chart: Chart; songs: Item[]; children?: ReactNode }) {
  const { play, current, playing } = usePlayer();
  const podium = songs.slice(0, 3);
  return (
    <>
      <header className="chart-head sr-only">
        <h2 className="sr-only">{chart.name}</h2>
      </header>
      <div className="podium">
        {podium.map((item, i) => (
          <button key={`${i}-${item.copies[0]?.id}`} className={`podium-item place-${i + 1}`} onClick={() => play(songs, i)}>
            {i === 0 && item.cover && <span className="podium-bg" style={{ backgroundImage: `url("${sized(item.cover, 200)}")` }} />}
            <span className="podium-rank">{i + 1}</span>
            <Cover src={item.cover} size={i === 0 ? 256 : 110} />
            <span className="podium-text">
              {i === 0 && <span className="eyebrow">本期第一</span>}
              <strong>{item.title}</strong>
              <small>{artistLine(item)}</small>
            </span>
          </button>
        ))}
      </div>
      <div className="chart-layout">
        <ol className="rank-list">
          {songs.slice(3).map((item, k) => {
            const i = k + 3;
            const active = current !== null && sameItem(current, item);
            return (
              <li
                key={`${i}-${item.copies[0]?.id}`}
                className={active ? 'is-current' : ''}
                onClick={() => play(songs, i)}
                tabIndex={0}
                onKeyDown={(e) => e.key === 'Enter' && play(songs, i)}
              >
                <span className={`rank-number${i < 10 ? ' is-top' : ''}`}>{active && playing ? <Bars /> : i + 1}</span>
                <Cover src={item.cover} size={44} />
                <span className="song-text">
                  <strong>{item.title}</strong>
                  <small>{artistLine(item)}</small>
                </span>
                <span className="song-album">{item.album}</span>
                <span className="song-actions">
                  <Heart item={item} />
                  <span className="song-time">{clock(item.duration)}</span>
                  <SongMenu item={item} />
                </span>
              </li>
            );
          })}
        </ol>
        {children}
      </div>
    </>
  );
}
