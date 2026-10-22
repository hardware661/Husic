import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Cover } from './components';
import { Icon } from './icons';
import { imageBlob } from './local-image';
import { toast } from './toast';

const covers=new Map<string,string>(),loaded=new Set<string>(),listeners=new Set<()=>void>(),revisions=new Map<string,number>();
function database():Promise<IDBDatabase>{return new Promise((resolve,reject)=>{
  const r=indexedDB.open('husic-covers',1);r.onupgradeneeded=()=>r.result.createObjectStore('covers');r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(new Error('这台设备暂时不能保存封面'));
});}
function display(key:string,blob:Blob){const old=covers.get(key);if(old)URL.revokeObjectURL(old);covers.set(key,URL.createObjectURL(blob));listeners.forEach(f=>f());}
async function load(key:string){
  if(loaded.has(key))return;loaded.add(key);const revision=revisions.get(key);let db:IDBDatabase|undefined;
  try{db=await database();const blob=await new Promise<unknown>((resolve,reject)=>{const r=db!.transaction('covers').objectStore('covers').get(key);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
    if(revision===revisions.get(key)&&blob instanceof Blob&&blob.type==='image/jpeg'&&blob.size<=10*1024*1024)display(key,blob);
  }catch{loaded.delete(key);}finally{db?.close();}
}
export function useCollectionImage(key:string){
  useEffect(()=>{void load(key);},[key]);
  return useSyncExternalStore(f=>{listeners.add(f);return()=>listeners.delete(f);},()=>covers.get(key)||'');
}
async function save(key:string,file:File){
  const revision=(revisions.get(key)||0)+1;revisions.set(key,revision);
  const blob=await imageBlob(file,1000);if(revisions.get(key)!==revision)return;
  const db=await database();try{await new Promise<void>((resolve,reject)=>{const tx=db.transaction('covers','readwrite');tx.objectStore('covers').put(blob,key);tx.oncomplete=()=>resolve();tx.onabort=tx.onerror=()=>reject(new Error('封面没有保存，请检查设备存储空间'));});}finally{db.close();}
  if(revisions.get(key)===revision)display(key,blob);
}
export function CollectionCover({coverKey,title,src,liked=false}:{coverKey:string;title:string;src?:string;liked?:boolean}){
  const custom=useCollectionImage(coverKey),input=useRef<HTMLInputElement>(null),[busy,setBusy]=useState(false);
  return <div className="collection-cover-wrap"><button type="button" className={`collection-art collection-cover${liked?' liked':''}`} aria-label={`更换${title}封面`} disabled={busy} onClick={()=>input.current?.click()}>
    {custom||src?<Cover src={custom||src} size={480} eager/>:<Icon name={liked?'heartFill':'note'} size={58}/>}
    <span className="cover-upload-mark" aria-hidden="true"><Icon name="plus" size={34}/></span>
  </button><input type="file" ref={input} accept="image/jpeg,image/png,image/webp,image/avif" hidden aria-label={`上传${title}封面`} onChange={event=>{const file=event.currentTarget.files?.[0];if(!file)return;setBusy(true);void save(coverKey,file).catch(e=>toast(e instanceof Error?e.message:'封面没有保存')).finally(()=>{setBusy(false);if(input.current)input.current.value='';});}}/></div>;
}
