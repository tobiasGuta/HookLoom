'use strict';
// HookLoom deliberately uses plain DOM APIs; captured content is never parsed as HTML.
let adminToken='';
let selected='';
let selectedEvent='';
let requestTab='overview';
let editing=false;
let currentSession=null;
let renderedToken='';
let refreshInProgress=false;
const $=id=>document.getElementById(id);
const el=(tag,cls,text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=String(text);return n};
const notify=message=>{$('notice').textContent=message};
const presets=[
 ['JSON 200',{status:200,headers:{'Content-Type':'application/json; charset=utf-8'},body:'{"ok":true}'}],
 ['Text 200',{status:200,headers:{'Content-Type':'text/plain; charset=utf-8'},body:'Hello from HookLoom'}],
 ['HTML 200',{status:200,headers:{'Content-Type':'text/html; charset=utf-8'},body:'<!doctype html><title>HookLoom</title><h1>It works</h1>'}],
 ['Redirect 302',{status:302,headers:{Location:'/done'},body:''}],
 ['No content 204',{status:204,headers:{},body:''}],
 ['Not found 404',{status:404,headers:{'Content-Type':'text/plain; charset=utf-8'},body:'Not found'}],
 ['Error 500',{status:500,headers:{'Content-Type':'text/plain; charset=utf-8'},body:'Server error'}]
];
function button(label,cls,click){const b=el('button',cls,label);b.type='button';b.addEventListener('click',click);return b}
function fmtDate(v){return new Date(v).toLocaleString()}
function fmtTime(v){return new Date(v).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit',second:'2-digit'})}
function remaining(v){const sec=Math.max(0,Math.floor((new Date(v).getTime()-Date.now())/1000));return sec>60?Math.ceil(sec/60)+' min remaining':sec+' sec remaining'}
function setTheme(theme){document.documentElement.dataset.theme=theme;$('themeToggle').textContent=theme==='dark'?'Light mode':'Dark mode';try{localStorage.setItem('hookloom-theme',theme)}catch{}}
(function initTheme(){let theme='light';try{theme=localStorage.getItem('hookloom-theme')||'light'}catch{}setTheme(theme==='dark'?'dark':'light')})();
$('themeToggle').addEventListener('click',()=>setTheme(document.documentElement.dataset.theme==='dark'?'light':'dark'));
async function api(path,options={}){
 const headers={'Authorization':'Bearer '+adminToken};
 if(options.body!==undefined)headers['Content-Type']='application/json';
 const res=await fetch('/api/'+path,{...options,headers,cache:'no-store'});
 const data=await res.json();
 if(!res.ok)throw new Error(data.error||res.statusText);
 return data;
}
function discardPrompt(){return !editing||confirm('Discard your unsaved response changes?')}
async function login(){
 adminToken=$('adminToken').value.trim();$('adminToken').value='';
 if(!adminToken)return;
 try{await load();$('loginPane').classList.add('hidden');$('workspace').classList.remove('hidden')}
 catch(e){adminToken='';alert('Authentication failed: '+e.message)}
}
$('unlock').addEventListener('click',login);
$('adminToken').addEventListener('keydown',e=>{if(e.key==='Enter')login()});
$('lock').addEventListener('click',()=>{
 if(!discardPrompt())return;
 adminToken='';selected='';selectedEvent='';currentSession=null;renderedToken='';editing=false;
 $('workspace').classList.add('hidden');$('loginPane').classList.remove('hidden');
 $('sessionPane').replaceChildren();$('requestList').replaceChildren();$('requestDetail').replaceChildren();
});
function openCreate(){if(!discardPrompt())return;$('createError').textContent='';$('createDialog').showModal();$('name').focus()}
$('openCreate').addEventListener('click',openCreate);
$('closeCreate').addEventListener('click',()=>$('createDialog').close());
$('cancelCreate').addEventListener('click',()=>$('createDialog').close());
$('emptyCreate').addEventListener('click',openCreate);
$('createForm').addEventListener('submit',async e=>{
 e.preventDefault();$('createError').textContent='';$('create').disabled=true;
 try{
  const s=await api('endpoints',{method:'POST',body:JSON.stringify({name:$('name').value,ttlMinutes:Number($('ttl').value),maxRequests:Number($('requestLimit').value),bodyLimitKiB:Number($('bodyLimit').value)})});
  selected=s.token;selectedEvent='';editing=false;$('createDialog').close();
  await load();notify('Endpoint created. Copy its URL to test incoming requests.');
 }catch(err){$('createError').textContent=err.message}
 finally{$('create').disabled=false}
});
$('refresh').addEventListener('click',()=>load({preserveEditor:true}).then(()=>notify('Request activity refreshed.')).catch(e=>notify(e.message)));
$('endpointSearch').addEventListener('input',()=>{if(currentSession)renderSidebar(lastList);else renderSidebar([])});
let lastList=[];
async function load({preserveEditor=false}={}){
 if(!adminToken||refreshInProgress)return;
 refreshInProgress=true;
 try{
  const all=await api('endpoints');lastList=all;
  if(all.length===0){selected='';currentSession=null;selectedEvent='';editing=false;renderedToken='';renderSidebar(all);renderEmpty();return}
  if(!selected||!all.some(s=>s.token===selected)){selected=all[0].token;selectedEvent='';editing=false;preserveEditor=false}
  const data=await api('endpoints/'+selected);currentSession=data;
  renderSidebar(all);
  if(renderedToken!==selected || (!preserveEditor && !editing)){renderSession(data);renderedToken=selected}
  renderInspector(data);
 }finally{refreshInProgress=false}
}
function renderSidebar(sessions){
 const list=$('sessionList');list.replaceChildren();
 $('endpointCount').textContent=String(sessions.length);
 const query=$('endpointSearch').value.trim().toLowerCase();
 const filtered=sessions.filter(s=>s.name.toLowerCase().includes(query));
 if(!filtered.length){list.append(el('p','empty-muted',sessions.length?'No matching endpoints.':'No endpoints yet.'));return}
 for(const s of filtered){
  const b=el('button','session-button'+(selected===s.token?' active':''));b.type='button';b.setAttribute('aria-pressed',String(selected===s.token));
  b.append(el('div','session-name',s.name));
  const meta=el('div','session-meta');meta.append(el('span','',s.seen+'/'+s.maxRequests+' requests'),el('span','','·'),el('span','',remaining(s.expiresAt)));b.append(meta);
  b.addEventListener('click',async()=>{
   if(s.token===selected)return;
   if(!discardPrompt())return;
   selected=s.token;selectedEvent='';requestTab='overview';editing=false;
   try{await load()}catch(e){notify(e.message)}
  });list.append(b);
 }
}
function renderEmpty(){
 const root=$('sessionPane');root.replaceChildren();
 const blank=el('div','blank-state');blank.append(el('div','blank-icon','↗'),el('h2','','Ready when you are'),el('p','','Create an endpoint to configure a response and inspect incoming requests.'));
 blank.append(button('Create endpoint','primary',openCreate));root.append(blank);
 $('captureCount').textContent='0';$('requestList').replaceChildren(el('div','empty-muted','Incoming requests will appear here.'));
 $('requestDetail').replaceChildren(el('div','empty-muted','Select a request to inspect its details.'));
}
function trackedField(label,value,id,type='text'){
 const wrapper=el('div');const l=el('label','',label);l.htmlFor=id;
 const field=el(type==='textarea'?'textarea':'input');field.id=id;
 if(type!=='textarea'){field.type=type;if(type==='number'){field.step='1'}}
 field.value=value;field.addEventListener('input',markEdited);wrapper.append(l,field);return wrapper;
}
function markEdited(){editing=true;const mark=$('unsaved');if(mark)mark.textContent='Unsaved changes'}
function addHeaderRow(name='',value=''){
 const row=el('div','header-row');row.className='header-row';
 const key=el('input');key.type='text';key.placeholder='Header name';key.value=name;key.setAttribute('aria-label','Response header name');
 const val=el('input');val.type='text';val.placeholder='Value';val.value=value;val.setAttribute('aria-label','Response header value');
 key.addEventListener('input',markEdited);val.addEventListener('input',markEdited);
 row.append(key,val,button('×','subtle',()=>{row.remove();markEdited()}));
 $('headerRows').append(row);
}
function replaceHeaderRows(headers){$('headerRows').replaceChildren();for(const [key,value] of Object.entries(headers||{}))addHeaderRow(key,value)}
function readHeaders(){
 const headers={};const seen=new Set();
 for(const row of $('headerRows').children){
  const [key,val]=row.querySelectorAll('input');const name=key.value.trim();
  if(!name&&!val.value)continue;
  if(!name)throw new Error('A response header is missing its name.');
  const canonical=name.toLowerCase();if(seen.has(canonical))throw new Error('Duplicate response header: '+name);
  seen.add(canonical);headers[name]=val.value;
 }
 return headers;
}
function applyPreset(spec,label){
 $('responseStatus').value=String(spec.status);$('responseDelay').value='0';$('responseBody').value=spec.body;
 replaceHeaderRows(spec.headers);markEdited();notify(label+' preset loaded. Save changes to activate it.');
}
function renderSession(s){
 const root=$('sessionPane');root.replaceChildren();
 const head=el('div','detail-title-row');head.append(el('h2','',s.name),el('span','pill',remaining(s.expiresAt)));root.append(head);
 const stats=el('div','session-stats');stats.append(el('span','',s.seen+'/'+s.maxRequests+' requests'),el('span','',s.captures.length+' retained'),el('span','',s.bodyLimitKiB+' KiB body limit'));root.append(stats);
 root.append(el('span','field-label','Capture URL'));
 const url=el('div','endpoint-url');url.append(el('code','',s.url),button('Copy','subtle small-btn',()=>navigator.clipboard.writeText(s.url).then(()=>notify('Capture URL copied.')).catch(()=>notify('Copy failed: copy the URL manually.'))));root.append(url);
 const toolbar=el('div','actions-row');toolbar.append(button('Export requests','subtle small-btn',()=>exportSession(s.token)),button('Delete endpoint','danger small-btn',()=>deleteSession(s.token)));root.append(toolbar);
 root.append(el('hr','section-divider'));
 const section=el('div','section-title');section.append(el('h3','','Response builder'),el('span','help-inline','Applies to future requests'));root.append(section);
 const presetHelp=el('p','help-inline','Start from a preset, then customize and save.');root.append(presetHelp);
 const bar=el('div','preset-bar');for(const [label,spec] of presets)bar.append(button(label,'',()=>applyPreset(spec,label)));root.append(bar);
 const grid=el('div','form-grid');grid.append(trackedField('HTTP status',String(s.response.status),'responseStatus','number'),trackedField('Delay (ms, up to 2000)',String(s.response.delayMs),'responseDelay','number'));root.append(grid);
 $('responseStatus').min='200';$('responseStatus').max='599';$('responseDelay').min='0';$('responseDelay').max='2000';
 const headers=el('div','field-group');headers.append(el('label','','Response headers'));
 const rows=el('div');rows.id='headerRows';headers.append(rows);headers.append(button('+ Add header','secondary-link',()=>{addHeaderRow();markEdited()}));root.append(headers);replaceHeaderRows(s.response.headers);
 const body=el('div','field-group');body.append(trackedField('Response body (exact text)',s.response.body||'','responseBody','textarea'));root.append(body);
 const rules=el('details','rules-box');const summary=el('summary','',`Conditional rules · ${s.rules?.length||0} configured`);rules.append(summary);
 const rulesContent=el('div','rules-content');rulesContent.append(el('p','','Advanced: provide a JSON array of first-match rules. A matching rule overrides the default response.'));
 rulesContent.append(trackedField('Rules JSON',JSON.stringify(s.rules||[],null,2),'rulesJSON','textarea'));rules.append(rulesContent);if(s.rules?.length)rules.open=true;root.append(rules);
 const save=el('div','save-row');const unsaved=el('span','unsaved','');unsaved.id='unsaved';save.append(unsaved,button('Save response','primary',()=>saveSettings(s.token)));root.append(save);
}
function renderInspector(s){
 $('captureCount').textContent=String(s.captures.length);
 const list=$('requestList');list.replaceChildren();
 const reverse=[...s.captures].reverse();
 if(!reverse.length){selectedEvent='';list.append(el('div','empty-muted','Waiting for your first request. Send HTTP traffic to the capture URL.'));$('requestDetail').replaceChildren(el('div','empty-muted','Select a captured request to inspect its details.'));return}
 if(!selectedEvent||!reverse.some(ev=>ev.id===selectedEvent)){selectedEvent=reverse[0].id;requestTab='overview'}
 for(const event of reverse){
  const b=el('button','request-button'+(selectedEvent===event.id?' active':''));b.type='button';b.setAttribute('aria-pressed',String(selectedEvent===event.id));
  const line=el('div','request-main');line.append(el('span','method',event.method),el('span','request-path',event.path||'/'));b.append(line,el('div','request-timestamp',fmtTime(event.at)));
  b.addEventListener('click',()=>{selectedEvent=event.id;requestTab='overview';renderInspector(s)});list.append(b);
 }
 const ev=reverse.find(item=>item.id===selectedEvent);renderRequestDetail(ev);
}
function renderRequestDetail(ev){
 const root=$('requestDetail');root.replaceChildren();
 root.append(el('h3','','Request details'),el('p','',ev.method+' '+(ev.path||'/')+' · '+fmtDate(ev.at)));
 const tabs=el('div','request-tabs');tabs.setAttribute('role','tablist');
 const content=el('div','request-content');
 for(const [id,label] of [['overview','Overview'],['headers','Headers'],['query','Query'],['body','Body'],['raw','Raw']]){
  const b=button(label,'request-tab'+(requestTab===id?' active':''),()=>{requestTab=id;renderRequestDetail(ev)});b.setAttribute('role','tab');b.setAttribute('aria-selected',String(requestTab===id));tabs.append(b);
 }
 root.append(tabs,content);
 if(requestTab==='overview'){
  const dl=el('dl','data-grid');
  const fields=[['Method',ev.method],['Path',ev.path||'/'],['Captured',fmtDate(ev.at)],['Body size',ev.bodyBytes+' bytes'],['Encoding',ev.bodyEncoding],['Truncated',ev.truncated?'Yes':'No'],['Response rule',ev.matchedRule>=0?'Rule '+(ev.matchedRule+1):'Default']];
  for(const [label,value] of fields)dl.append(el('dt','',label),el('dd','',value));content.append(dl);
 }else if(requestTab==='headers'||requestTab==='query'){
  const data=requestTab==='headers'?ev.headers:ev.query;const items=Object.entries(data||{});
  if(!items.length){content.append(el('p','empty-muted','No '+requestTab+' data for this request.'));return}
  const all=el('div','kv-list');for(const [key,value] of items){const item=el('div','kv-item');item.append(el('strong','',key),el('span','',Array.isArray(value)?value.join(', '):String(value)));all.append(item)}content.append(all);
 }else if(requestTab==='body'){
  if(!ev.body){content.append(el('p','empty-muted','This request has no body.'));return}
  if(ev.bodyEncoding==='base64')content.append(el('p','body-note','Binary body — displayed as base64.'));
  let shown=ev.body;
  if(ev.bodyEncoding!=='base64'){try{shown=JSON.stringify(JSON.parse(shown),null,2)}catch{}}
  content.append(el('pre','',shown));
 }else content.append(el('pre','',JSON.stringify(ev,null,2)));
}
async function saveSettings(token){
 try{
  const payload={response:{status:Number($('responseStatus').value),delayMs:Number($('responseDelay').value),headers:readHeaders(),body:$('responseBody').value},rules:JSON.parse($('rulesJSON').value)};
  if(!Array.isArray(payload.rules))throw new Error('Rules must be a JSON array.');
  await api('endpoints/'+token,{method:'PUT',body:JSON.stringify(payload)});
  editing=false;await load();notify('Response saved. New requests will use these settings.');
 }catch(e){notify('Could not save: '+e.message)}
}
async function deleteSession(token){
 if(!confirm('Destroy this endpoint and all in-memory requests?'))return;
 try{await api('endpoints/'+token,{method:'DELETE'});selected='';selectedEvent='';editing=false;await load();notify('Endpoint deleted.')}catch(e){notify(e.message)}
}
async function exportSession(token){
 try{
  const data=await api('endpoints/'+token+'/export');const blob=new Blob([JSON.stringify(data,null,2)],{type:'application/json'});
  const u=URL.createObjectURL(blob),anchor=el('a');anchor.href=u;anchor.download='hookloom-'+token.slice(0,8)+'.json';document.body.append(anchor);anchor.click();anchor.remove();setTimeout(()=>URL.revokeObjectURL(u),1000);
  notify('Export downloaded. Handle captured secrets carefully.');
 }catch(e){notify(e.message)}
}
setInterval(()=>{
 if(adminToken&&!$('workspace').classList.contains('hidden')){
  load({preserveEditor:true}).catch(e=>notify('Refresh failed: '+e.message));
 }
},5000);
