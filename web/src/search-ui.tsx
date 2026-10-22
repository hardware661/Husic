import { useEffect, useRef, useState, type FormEvent, type RefObject } from 'react';
import { Icon } from './icons';
import { navigate } from './router';
import { reduceMotion } from './preferences';
import { openSearch, rememberSearch, removeSearch, searchHistory, searchMusic, searchSuggestions } from './search-data';
export { openSearch };

type Choice={text:string;kind:'history'|'song'|'artist'};
export function SearchField({ initial='', onSearch, inputRef, overlay=false }: {initial?:string;onSearch:(q:string)=>void;inputRef?:RefObject<HTMLInputElement|null>;overlay?:boolean}) {
  const [text,setText]=useState(initial),[choices,setChoices]=useState<Choice[]>([]),[active,setActive]=useState(-1),[focused,setFocused]=useState(overlay);
  const [historyVersion,setHistoryVersion]=useState(0);
  const keyboardChoice=useRef(false);
  const ownRef=useRef<HTMLInputElement>(null),root=useRef<HTMLDivElement>(null);const ref=inputRef||ownRef;
  useEffect(()=>setText(initial),[initial]);
  useEffect(()=>{
    let live=true;setActive(-1);
    const q=text.trim();const history=searchHistory().filter(s=>!q||s.toLowerCase().includes(q.toLowerCase())).map(text=>({text,kind:'history' as const}));
    setChoices(history.slice(0,6));
    if(!q||!focused)return;
    const controller=new AbortController();
    const timer=setTimeout(()=>void searchSuggestions(q,controller.signal).then(songs=>{
      if(!live)return;
      const seen=new Set<string>();const all:Choice[]=[...history];
      for(const item of songs.slice(0,8)){all.push({text:item.title,kind:'song'});for(const artist of item.artists)if(artist.toLowerCase().includes(q.toLowerCase()))all.push({text:artist,kind:'artist'});}
      setChoices(all.filter(c=>{const key=c.text.toLowerCase();if(seen.has(key))return false;seen.add(key);return true;}).slice(0,6));
    }).catch(()=>{}),90);
    return()=>{live=false;clearTimeout(timer);controller.abort();};
  },[text,focused,historyVersion]);
  useEffect(()=>{const outside=(e:PointerEvent)=>{if(!root.current?.contains(e.target as Node))setFocused(false);};document.addEventListener('pointerdown',outside);return()=>document.removeEventListener('pointerdown',outside);},[]);
  const submit=(q=text)=>{q=q.trim();if(!q)return;setFocused(false);rememberSearch(q);void searchMusic(q).catch(()=>{});onSearch(q);};
  const list=focused&&choices.length>0;
  const deleteHistory=(q?:string)=>{removeSearch(q);keyboardChoice.current=false;setHistoryVersion(i=>i+1);ref.current?.focus();};
  const onSubmit=(e:FormEvent)=>{e.preventDefault();submit(keyboardChoice.current&&active>=0&&choices[active]?choices[active].text:text);};
  return <div className={`search-composer${overlay?' is-overlay':''}`} ref={root}>
    <form className="search-field" role="search" onSubmit={onSubmit}>
      <Icon name="search" size={21}/><input ref={ref} value={text} onChange={e=>setText(e.target.value)} onFocus={()=>setFocused(true)} role="combobox" aria-autocomplete="list" aria-expanded={list} aria-controls={overlay?'search-overlay-options':'search-page-options'} aria-activedescendant={active>=0?`${overlay?'overlay':'page'}-search-option-${active}`:undefined} aria-label="搜索音乐" placeholder="搜索音乐" enterKeyHint="search" autoComplete="off" onKeyDown={e=>{
        if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();keyboardChoice.current=true;setActive(i=>choices.length?(i<0?(e.key==='ArrowDown'?0:choices.length-1):(i+(e.key==='ArrowDown'?1:-1)+choices.length)%choices.length):-1);}
        if(e.key==='Escape'&&!overlay){e.preventDefault();setFocused(false);ref.current?.blur();}
      }}/>
      {text&&<button className="icon-button" type="button" aria-label="清除搜索" onClick={()=>{setText('');ref.current?.focus();}}><Icon name="close" size={16}/></button>}
      <button className="search-submit icon-button" type="submit" aria-label="搜索"><Icon name="chevron" size={20}/></button>
    </form>
    <div className={`search-options-panel${!text.trim()?' is-history':''}`} hidden={!list}>
      {choices.some(c=>c.kind==='history')&&<div className="search-history-tools"><button type="button" aria-label="清空搜索历史" onMouseDown={e=>e.preventDefault()} onClick={()=>deleteHistory()}>清空</button></div>}
      <div className="search-options" id={overlay?'search-overlay-options':'search-page-options'} role="listbox" aria-label="搜索建议">
        {choices.map((c,i)=><div className="search-option-row" key={c.text} role="presentation">
          <button type="button" id={`${overlay?'overlay':'page'}-search-option-${i}`} role="option" aria-selected={active===i} tabIndex={-1} onPointerMove={()=>{keyboardChoice.current=false;setActive(i);}} onMouseDown={e=>e.preventDefault()} onClick={()=>{setText(c.text);submit(c.text);}}><Icon name={c.kind==='history'?'clock':c.kind==='song'?'note':'search'} size={16}/><span>{c.text}</span>{c.kind!=='history'&&<Icon name="chevron" size={14}/>}</button>
          {c.kind==='history'&&<button type="button" className="search-history-remove" aria-label={`删除搜索历史：${c.text}`} onMouseDown={e=>e.preventDefault()} onClick={()=>deleteHistory(c.text)}><Icon name="close" size={14}/></button>}
        </div>)}
      </div>
    </div>
  </div>;
}

export function SearchOverlay({ onCommit }: {onCommit:()=>void}) {
  const [opened,setOpened]=useState(false),[docking,setDocking]=useState(false);
  const dialog=useRef<HTMLDialogElement>(null),panel=useRef<HTMLDivElement>(null),input=useRef<HTMLInputElement>(null),trigger=useRef<HTMLElement|null>(null),animation=useRef<Animation|null>(null),committing=useRef(false);
  const close=()=>{animation.current?.cancel();committing.current=false;dialog.current?.close();delete document.documentElement.dataset.searchTransition;setOpened(false);setDocking(false);};
  useEffect(()=>{const open=()=>{trigger.current=document.activeElement as HTMLElement;setOpened(true);setDocking(false);};window.addEventListener('husic-open-search',open);return()=>window.removeEventListener('husic-open-search',open);},[]);
  useEffect(()=>{
    if(!opened)return;dialog.current?.showModal();input.current?.focus();
    const key=(e:KeyboardEvent)=>{if(e.key==='Escape'){e.preventDefault();e.stopImmediatePropagation();close();trigger.current?.focus();}};
    document.addEventListener('keydown',key,true);return()=>document.removeEventListener('keydown',key,true);
  },[opened]);
  const submit=async(q:string)=>{
    if(committing.current)return;committing.current=true;
    const p=panel.current, start=p?.getBoundingClientRect();setDocking(true);document.documentElement.dataset.searchTransition='true';onCommit();navigate(`/search?q=${encodeURIComponent(q)}`);
    await new Promise<void>(r=>requestAnimationFrame(()=>requestAnimationFrame(()=>r())));
    if(!committing.current)return;
    const target=document.querySelector('.search-page .search-field')?.getBoundingClientRect();
    if(p&&start&&target&&!reduceMotion()){
      Object.assign(p.style,{left:`${start.x}px`,top:`${start.y}px`,width:`${start.width}px`,transform:'none'});
      animation.current=p.animate([{transform:'translate(0,0)',width:`${start.width}px`},{transform:`translate(${target.x-start.x}px,${target.y-start.y}px)`,width:`${target.width}px`}],{duration:330,easing:'cubic-bezier(.22,1,.36,1)',fill:'forwards'});
      await animation.current.finished.catch(()=>{});
    }
    if(committing.current)close();
  };
  return opened?<dialog ref={dialog} className={`search-command${docking?' is-docking':''}`} aria-label="搜索音乐" onCancel={e=>{e.preventDefault();close();}} onClick={e=>{if(e.target===dialog.current)close();}}>
    <div className="search-command-panel" ref={panel}><SearchField onSearch={q=>void submit(q)} inputRef={input} overlay/></div>
  </dialog>:null;
}
