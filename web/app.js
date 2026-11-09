'use strict';
// HookLoom deliberately uses plain DOM APIs; captured content is never parsed as HTML.
let adminToken='';
let selected='';
let selectedEvent='';
let requestTab='overview';
let centerTab='overview';
let editing=false;
let currentSession=null;
let renderedToken='';
let refreshInProgress=false;
let ruleMode='visual';
let visualRules=[];
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
function fmtSize(bytes){return bytes>=1024?(bytes/1024).toFixed(bytes>=10240?0:1)+' KiB':bytes+' B'}
function remaining(v){const sec=Math.max(0,Math.floor((new Date(v).getTime()-Date.now())/1000));return sec>60?Math.ceil(sec/60)+' min remaining':sec+' sec remaining'}
function compactRemaining(v){const sec=Math.max(0,Math.floor((new Date(v).getTime()-Date.now())/1000));return sec>60?Math.ceil(sec/60)+' min':sec+' sec'}
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
 try{await load();$('loginPane').classList.add('hidden');$('workspace').classList.remove('hidden');$('lock').classList.remove('hidden')}
 catch(e){adminToken='';alert('Authentication failed: '+e.message)}
}
$('unlock').addEventListener('click',login);
$('adminToken').addEventListener('keydown',e=>{if(e.key==='Enter')login()});
$('lock').addEventListener('click',()=>{
 if(!discardPrompt())return;
 adminToken='';selected='';selectedEvent='';currentSession=null;renderedToken='';editing=false;
 $('requestSearch').value='';$('requestMethod').value='';$('requestRule').value='';
 $('workspace').classList.add('hidden');$('loginPane').classList.remove('hidden');$('lock').classList.add('hidden');
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
  selected=s.token;selectedEvent='';centerTab='overview';editing=false;$('createDialog').close();
  await load();notify('Endpoint created. Copy its URL to test incoming requests.');
 }catch(err){$('createError').textContent=err.message}
 finally{$('create').disabled=false}
});
$('refresh').addEventListener('click',()=>load({preserveEditor:true}).then(()=>notify('Request activity refreshed.')).catch(e=>notify(e.message)));
$('requestSearch').addEventListener('input',()=>{if(currentSession)renderInspector(currentSession)});
$('requestMethod').addEventListener('change',()=>{if(currentSession)renderInspector(currentSession)});
$('requestRule').addEventListener('change',()=>{if(currentSession)renderInspector(currentSession)});
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
  renderSummary(data);
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
   selected=s.token;selectedEvent='';requestTab='overview';centerTab='overview';editing=false;
   try{await load()}catch(e){notify(e.message)}
  });list.append(b);
 }
}
function renderSummary(s){
 $('summaryUrl').textContent=s.url;
 $('summaryUrl').title=s.url;
 $('summaryExpires').textContent=compactRemaining(s.expiresAt);
 $('summaryRequests').textContent=String(s.seen);
 $('summaryCopy').disabled=false;
}
$('summaryCopy').addEventListener('click',()=>{
 if(!currentSession)return;
 navigator.clipboard.writeText(currentSession.url).then(()=>notify('Capture URL copied.')).catch(()=>notify('Could not copy. Select the URL manually.'));
});
function renderEmpty(){
 $('summaryUrl').textContent='Select an endpoint';$('summaryUrl').title='';$('summaryExpires').textContent='—';$('summaryRequests').textContent='0';$('summaryCopy').disabled=true;
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
function markEdited(){editing=true;const mark=$('unsaved');if(mark){mark.textContent='Unsaved changes';mark.classList.add('pending')}}
function addHeaderRow(name='',value=''){
 const row=el('div','header-row');row.className='header-row';
 const key=el('input');key.type='text';key.placeholder='Header name';key.value=name;key.setAttribute('aria-label','Response header name');
 const val=el('input');val.type='text';val.placeholder='Value';val.value=value;val.setAttribute('aria-label','Response header value');
 key.addEventListener('input',markEdited);val.addEventListener('input',markEdited);
 const remove=button('×','icon-btn',()=>{row.remove();markEdited()});remove.setAttribute('aria-label','Remove response header');row.append(key,val,remove);
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
 replaceHeaderRows(spec.headers);$('editorLanguage').textContent=guessLanguage(spec.headers);markEdited();updateLineNumbers();selectPreset(label);notify(label+' preset loaded. Save changes to activate it.');
}
function renderSession(s){
 const root=$('sessionPane');root.replaceChildren();
 const head=el('div','detail-title-row');const nameWrap=el('div','endpoint-heading');nameWrap.append(el('span','endpoint-icon','⬡'),el('h2','',s.name));head.append(nameWrap,el('span','active-status','● Active'));root.append(head);
 const url=el('div','endpoint-url');url.append(el('code','',s.url));
 const copy=button('⧉','icon-btn',()=>navigator.clipboard.writeText(s.url).then(()=>notify('Capture URL copied.')).catch(()=>notify('Copy failed. Select the URL manually.')));copy.title='Copy capture URL';copy.setAttribute('aria-label','Copy capture URL');
 const open=button('↗','icon-btn',()=>window.open(s.url,'_blank','noopener,noreferrer'));open.title='Open capture URL';open.setAttribute('aria-label','Open capture URL in new tab');
 url.append(copy,open);root.append(url);
 const stats=el('div','metric-grid');
 for(const [icon,label,value] of [['⇄','Requests received',String(s.seen)],['◷','Time remaining',compactRemaining(s.expiresAt)],['▤','Body limit',s.bodyLimitKiB+' KiB']]){
  const item=el('div','metric');item.append(el('span','metric-icon',icon));const text=el('div','metric-text');text.append(el('span','metric-label',label),el('strong','metric-value',value));item.append(text);stats.append(item);
 }
 root.append(stats);
 const toolbar=el('div','actions-row');toolbar.append(button('⇩  Export requests','subtle small-btn',()=>exportSession(s.token)),button('♧  Delete endpoint','danger small-btn',()=>deleteSession(s.token)));root.append(toolbar);
 const nav=el('nav','center-tabs');nav.setAttribute('aria-label','Endpoint workspace sections');
 for(const [id,label,icon] of [['overview','Overview','⌂'],['response','Response','</>'],['rules','Rules','▱'],['requests','Requests','▥']]){
  const title=label+(id==='requests'?'  '+s.captures.length:'');
  const b=button(icon+'  '+title,'center-tab'+(centerTab===id?' active':''),()=>setCenterTab(id));b.dataset.tab=id;b.setAttribute('aria-current',String(centerTab===id));nav.append(b);
 }
 root.append(nav);
 const config=el('section','config-panel');config.id='responsePanel';root.append(config);config.append(el('h3','','Response configuration'),el('p','help-inline','Choose a preset or customize the response.'));
 const bar=el('div','preset-bar');bar.id='presetBar';for(const [label,spec] of presets){const b=button(label,'preset-button',()=>applyPreset(spec,label));b.dataset.preset=label;bar.append(b)}config.append(bar);
 const grid=el('div','form-grid');grid.append(trackedField('HTTP status',String(s.response.status),'responseStatus','number'),trackedField('Response delay (ms)',String(s.response.delayMs),'responseDelay','number'));config.append(grid);
 $('responseStatus').min='200';$('responseStatus').max='599';$('responseDelay').min='0';$('responseDelay').max='2000';
 const headers=el('div','field-group');headers.append(el('label','','Response headers'));
 const headerFrame=el('div','header-table');const headerTitle=el('div','header-table-head');headerTitle.append(el('span','','Name'),el('span','','Value'),el('span','sr-only','Remove'));headerFrame.append(headerTitle);
 const rows=el('div');rows.id='headerRows';headerFrame.append(rows);headers.append(headerFrame);headers.append(button('+  Add header','secondary-link',()=>{addHeaderRow();markEdited()}));config.append(headers);replaceHeaderRows(s.response.headers);
 const body=el('div','field-group');body.append(el('label','','Response body'));
 const editor=el('div','code-editor');const editorBar=el('div','editor-toolbar');const language=el('span','language-chip',guessLanguage(s.response.headers));language.id='editorLanguage';const format=button('✧  Format','format-button',formatResponseBody);format.title='Pretty-print valid JSON';editorBar.append(language,format);editor.append(editorBar);
 const codeInput=el('div','code-input');const nums=el('div','code-lines','1');nums.id='editorLines';nums.setAttribute('aria-hidden','true');const textarea=el('textarea');textarea.id='responseBody';textarea.value=s.response.body||'';textarea.spellcheck=false;textarea.setAttribute('aria-label','Response body (exact text)');textarea.addEventListener('input',()=>{markEdited();updateLineNumbers()});textarea.addEventListener('scroll',()=>{nums.scrollTop=textarea.scrollTop});codeInput.append(nums,textarea);editor.append(codeInput);body.append(editor);config.append(body);
 const preview=el('section','response-preview-panel hidden');preview.id='responsePreviewPanel';preview.append(el('h3','','Response preview'),el('p','help-inline','A preview of the configured response. The server also adds standard security headers.'));
 const pre=el('pre','response-preview');pre.id='previewHTTP';preview.append(pre,button('←  Edit response','subtle',()=>setCenterTab('overview')));root.append(preview);
 const rules=el('section','rules-panel hidden');rules.id='rulesPanel';
 const heading=el('div','rules-header');const headingText=el('div');headingText.append(el('h3','','Conditional response rules'),el('p','help-inline','Rules are evaluated from top to bottom. The first match wins.'));
 const switcher=el('div','rule-mode-switch');switcher.setAttribute('role','group');switcher.setAttribute('aria-label','Rule editor mode');
 const visualMode=button('Visual builder','subtle small-btn',()=>switchRuleMode('visual'));visualMode.id='visualMode';
 const jsonMode=button('Advanced JSON','subtle small-btn',()=>switchRuleMode('json'));jsonMode.id='jsonMode';
 switcher.append(visualMode,jsonMode);heading.append(headingText,switcher);rules.append(heading);
 const visual=el('div');visual.id='visualRules';rules.append(visual);
 const raw=el('div','hidden');raw.id='rawRules';raw.append(trackedField('Rules (JSON array)',JSON.stringify(s.rules||[],null,2),'rulesJSON','textarea'));raw.append(el('p','help-inline','Edit the complete rules array. Switch to Visual builder to validate and preview the rules.'));rules.append(raw);
 root.append(rules);ruleMode='visual';visualRules=(s.rules||[]).map(toVisualRule);renderRuleEditor();
 const captured=el('section','captured-panel hidden');captured.id='capturedPanel';captured.append(el('h3','','Captured requests'),el('p','help-inline','Select a request to inspect its contents in the panel on the right.'));
 const captures=el('div','capture-overview');
 for(const ev of [...s.captures].reverse().slice(0,20)){
  const b=button(ev.method+'  '+(ev.path||'/')+'   ·   '+fmtTime(ev.at),'capture-overview-item',()=>{selectedEvent=ev.id;requestTab='overview';renderInspector(currentSession);$('requestDetail').scrollIntoView({block:'nearest',behavior:'smooth'})});captures.append(b);
 }
 if(!s.captures.length)captures.append(el('p','empty-muted','No requests captured yet. Send a request to your capture URL.'));captured.append(captures);root.append(captured);
 const footer=el('div','save-row');const saved=el('span','saved-status','✓  Saved  ·  Response settings up to date');saved.id='unsaved';footer.append(saved,button('▣  Save changes','primary save-button',()=>saveSettings(s.token)));root.append(footer);
 updateLineNumbers();selectPreset(null);setCenterTab(centerTab);
}
function guessLanguage(headers){const type=Object.entries(headers||{}).find(([k])=>k.toLowerCase()==='content-type')?.[1]||'';return /json/i.test(type)?'JSON':/html/i.test(type)?'HTML':'TEXT'}
function updateLineNumbers(){const body=$('responseBody'),nums=$('editorLines');if(!body||!nums)return;const length=body.value.split('\n').length;nums.textContent=Array.from({length:Math.min(length,1500)},(_,i)=>i+1).join('\n');}
function selectPreset(label){const bar=$('presetBar');if(!bar)return;for(const b of bar.querySelectorAll('button'))b.classList.toggle('active',b.dataset.preset===label);}
function formatResponseBody(){const body=$('responseBody');try{body.value=JSON.stringify(JSON.parse(body.value),null,2);$('editorLanguage').textContent='JSON';updateLineNumbers();markEdited();notify('JSON formatted. Save changes to apply.')}catch{notify('Only valid JSON can be formatted. Original body was kept.')}}
function renderResponsePreview(){
 const preview=$('previewHTTP');if(!preview)return;
 try{
  const status=$('responseStatus').value;
  const headers=readHeaders();const body=$('responseBody').value;
  preview.textContent='HTTP/1.1 '+status+'\n'+Object.entries(headers).map(([k,v])=>k+': '+v).join('\n')+'\n\n'+body;
 }catch(e){preview.textContent='Complete the response header fields to see a preview. '+e.message}
}
function renderCapturesSummary(s){
 const root=document.querySelector('#capturedPanel .capture-overview');if(!root)return;
 root.replaceChildren();
 for(const ev of [...s.captures].reverse().slice(0,20)){
  const b=button(ev.method+'  '+(ev.path||'/')+'   ·   '+fmtTime(ev.at),'capture-overview-item',()=>{selectedEvent=ev.id;requestTab='overview';renderInspector(currentSession);$('requestDetail').scrollIntoView({block:'nearest',behavior:'smooth'})});root.append(b);
 }
 if(!s.captures.length)root.append(el('p','empty-muted','No requests captured yet. Send a request to your capture URL.'));
 const counter=document.querySelector('.center-tab[data-tab="requests"]');if(counter)counter.textContent='▥  Requests  '+s.captures.length;
}
function setCenterTab(tab){centerTab=tab;
 const config=$('responsePanel'),preview=$('responsePreviewPanel'),rules=$('rulesPanel'),captured=$('capturedPanel');if(!config||!rules||!captured||!preview)return;
 config.classList.toggle('hidden',tab!=='overview');preview.classList.toggle('hidden',tab!=='response');rules.classList.toggle('hidden',tab!=='rules');captured.classList.toggle('hidden',tab!=='requests');
 if(tab==='response')renderResponsePreview();
 for(const b of document.querySelectorAll('.center-tab')){const active=b.dataset.tab===tab;b.classList.toggle('active',active);b.setAttribute('aria-current',String(active));}
}
function renderInspector(s){
 renderCapturesSummary(s);
 const all=[...s.captures].reverse();
 $('captureCount').textContent=String(all.length);
 const list=$('requestList');list.replaceChildren();
 const query=$('requestSearch').value.trim().toLowerCase();
 const method=$('requestMethod').value;
 const matched=$('requestRule').value;
 const filtered=all.filter(ev=>{
  const m=String(ev.method||'').toUpperCase();
  const methodMatches=!method||(method==='OTHER'?!['GET','POST','PUT','PATCH','DELETE','HEAD','OPTIONS'].includes(m):m===method);
  const text=(String(ev.path||'/')+' '+JSON.stringify(ev.query||{})+' '+JSON.stringify(ev.headers||{})+' '+String(ev.body||'')+' '+m).toLowerCase();
  const ruleMatches=!matched||(matched==='matched'?ev.matchedRule>=0:ev.matchedRule<0);
  return methodMatches&&ruleMatches&&(!query||text.includes(query));
 });
 if(!all.length){selectedEvent='';list.append(el('div','empty-muted','No requests yet. Send traffic to the capture URL to see it here.'));$('requestDetail').replaceChildren(el('div','empty-muted','Select a captured request to inspect its details.'));return}
 if(!filtered.length){selectedEvent='';list.append(el('div','empty-muted','No requests match your filters.'));$('requestDetail').replaceChildren(el('div','empty-muted','Try changing the search or method filter.'));return}
 if(!selectedEvent||!filtered.some(ev=>ev.id===selectedEvent)){selectedEvent=filtered[0].id;requestTab='overview'}
 for(const event of filtered){
  const b=el('button','request-button'+(selectedEvent===event.id?' active':''));b.type='button';b.setAttribute('aria-pressed',String(selectedEvent===event.id));
  const m=el('span','method',event.method);m.dataset.method=event.method;
  b.append(m,el('span','request-path',event.path||'/'),el('span','request-size',fmtSize(event.bodyBytes||0)),el('span','request-time',fmtTime(event.at)));
  b.title=event.matchedRule>=0?'Matched conditional rule '+(event.matchedRule+1):'Used default response';
  b.addEventListener('click',()=>{selectedEvent=event.id;requestTab='overview';renderInspector(s)});list.append(b);
 }
 renderRequestDetail(filtered.find(item=>item.id===selectedEvent));
}
function renderRequestDetail(ev){
 const root=$('requestDetail');root.replaceChildren();
 const head=el('div','request-detail-heading');const h=el('h3','','▤  Request details');
 const copy=button('Copy JSON','subtle small-btn',()=>{
  if(!navigator.clipboard?.writeText){notify('Clipboard unavailable. Open Raw to copy the request manually.');return}
  navigator.clipboard.writeText(JSON.stringify(ev,null,2)).then(()=>notify('Request JSON copied. It may contain sensitive headers or bodies.')).catch(()=>notify('Clipboard unavailable. Open Raw to copy the request manually.'));
 });
 copy.title='Copy this captured request as JSON (may include secrets)';head.append(h,copy);root.append(head,el('p','',ev.method+' '+(ev.path||'/')+' · '+fmtDate(ev.at)));
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
// Visual editor state stays in memory until the user clicks Save changes.
// Every existing condition and response field maps to the unchanged Go API.
function toVisualRule(rule){
 const m={...(rule.match||{})},r=rule.response||{};
 return {match:m,pathMode:m.pathExact?'exact':'prefix',matchAll:!Object.values(m).some(value=>String(value||'').length>0),response:{status:r.status??200,delayMs:r.delayMs??0,body:r.body??''},headerRows:Object.entries(r.headers||{}).map(([name,value])=>({name,value}))};
}
function isMatchDefined(match){return Object.values(match).some(value=>String(value||'').trim().length>0)}
function readVisualRules(){
 return visualRules.map((draft,index)=>{
  const label='Rule '+(index+1);
  const match=draft.matchAll?{}:Object.fromEntries(Object.entries(draft.match).map(([k,v])=>[k,String(v??'').trim()]).filter(([,v])=>v));
  if(!draft.matchAll&&!isMatchDefined(match))throw new Error(label+': set a condition or explicitly enable Match all requests.');
  if(match.queryValue&&!match.queryKey)throw new Error(label+': a query value requires a query key.');
  if(match.headerValue&&!match.headerKey)throw new Error(label+': a header value requires a header key.');
  if(match.pathPrefix&&!match.pathPrefix.startsWith('/'))throw new Error(label+': path prefix must start with /.');
  if(match.pathExact&&!match.pathExact.startsWith('/'))throw new Error(label+': exact path must start with /.');
  if(match.pathPrefix&&match.pathExact)throw new Error(label+': choose either Starts with or Exact match.');
  if(!draft.matchAll&&draft.pathMode==='exact'&&!match.pathExact)throw new Error(label+': enter a path when using Exact match.');
  if(match.method&&match.method!==match.method.toUpperCase())throw new Error(label+': HTTP method must be uppercase.');
  const headers={},seen=new Set();
  for(const row of draft.headerRows){
   const name=String(row.name||'').trim(),value=String(row.value||'');
   if(!name&&!value)continue;
   if(!name)throw new Error(label+': a response header is missing its name.');
   if(seen.has(name.toLowerCase()))throw new Error(label+': duplicate response header '+name+'.');
   seen.add(name.toLowerCase());headers[name]=value;
  }
  const status=Number(draft.response.status),delayMs=Number(draft.response.delayMs);
  if(!Number.isInteger(status)||status<200||status>599)throw new Error(label+': status must be 200–599.');
  if(!Number.isInteger(delayMs)||delayMs<0||delayMs>2000)throw new Error(label+': delay must be 0–2000 ms.');
  return {match,response:{status,delayMs,headers,body:String(draft.response.body||'')}};
 });
}
function readCurrentRules(){
 if(ruleMode==='visual')return readVisualRules();
 let parsed;try{parsed=JSON.parse($('rulesJSON').value)}catch{throw new Error('Advanced rules JSON is invalid.');}
 if(!Array.isArray(parsed))throw new Error('Advanced rules must be a JSON array.');
 if(parsed.length>12)throw new Error('At most 12 rules can be configured.');
 return parsed;
}
function switchRuleMode(mode){
 if(mode===ruleMode)return;
 try{
  if(mode==='json')$('rulesJSON').value=JSON.stringify(readVisualRules(),null,2);
  else{
   const rules=readCurrentRules();
   visualRules=rules.map(toVisualRule);
  }
  ruleMode=mode;renderRuleEditor();
 }catch(e){notify('Cannot switch rule editor: '+e.message)}
}
function ruleInput(label,value,apply,options={}){
 const wrapper=el('div','rule-field');const labelEl=el('label','',label);
 const field=el(options.multiline?'textarea':options.choices?'select':'input');
 if(options.multiline){field.rows=3;field.spellcheck=false}
 else if(options.choices){for(const [value,label] of options.choices){const opt=el('option','',label);opt.value=value;field.append(opt)}}
 else{field.type=options.type||'text';if(options.placeholder)field.placeholder=options.placeholder;if(options.type==='number'){field.step='1';if(options.min!==undefined)field.min=options.min;if(options.max!==undefined)field.max=options.max}}
 field.value=String(value??'');
 const eventName=field.tagName==='SELECT'?'change':'input';
 field.addEventListener(eventName,()=>{apply(field.value);markEdited()});
 labelEl.append(field);wrapper.append(labelEl);return wrapper;
}
function renderRuleEditor(){
 const visual=$('visualRules'),raw=$('rawRules');if(!visual||!raw)return;
 visual.classList.toggle('hidden',ruleMode!=='visual');raw.classList.toggle('hidden',ruleMode!=='json');
 $('visualMode').classList.toggle('mode-active',ruleMode==='visual');$('jsonMode').classList.toggle('mode-active',ruleMode==='json');
 if(ruleMode!=='visual')return;
 visual.replaceChildren();
 if(!visualRules.length)visual.append(el('div','rules-empty','No conditional rules yet. The endpoint returns its default response.'));
 visualRules.forEach((draft,index)=>{
  const card=el('article','rule-card');
  const top=el('div','rule-card-head');
  const title=el('div','rule-title');title.append(el('strong','',`Rule ${index+1}`),el('span','rule-priority',index===0?'First priority':`Priority ${index+1}`));
  const actions=el('div','rule-actions');
  const up=button('↑','subtle compact-icon',()=>moveRule(index,-1));up.title='Move rule up';up.setAttribute('aria-label',`Move rule ${index+1} up`);up.disabled=index===0;
  const down=button('↓','subtle compact-icon',()=>moveRule(index,1));down.title='Move rule down';down.setAttribute('aria-label',`Move rule ${index+1} down`);down.disabled=index===visualRules.length-1;
  const remove=button('Remove','danger small-btn',()=>{visualRules.splice(index,1);markEdited();renderRuleEditor()});
  actions.append(up,down,remove);top.append(title,actions);card.append(top);
  const condition=el('div','rule-subsection');condition.append(el('h4','','When this request matches'));
  const basic=el('div','rule-fields');
  const basicInputs=[['Method','method','GET, POST, HEAD…']];
  const extraInputs=[['Query key','queryKey','role'],['Query value','queryValue','admin'],['Header name','headerKey','X-Test'],['Header value','headerValue','yes'],['Body includes','bodyContains','substring']];
  const allLabel=el('label','rule-all-toggle');const all=el('input');all.type='checkbox';all.checked=draft.matchAll;
  all.addEventListener('change',()=>{draft.matchAll=all.checked;markEdited();renderRuleEditor()});allLabel.append(all,document.createTextNode('Match all requests (catch-all rule)'));condition.append(allLabel);
  for(const [label,key,placeholder] of basicInputs){
   const control=ruleInput(label,draft.match[key]||'',v=>{draft.match[key]=v},{placeholder});
   control.querySelector('input').disabled=draft.matchAll;basic.append(control);
  }
  // The dropdown chooses the match operator; only the selected path key is sent to Go.
  // Existing pathPrefix rules default to "Starts with" for backward compatibility.
  const pathControl=el('div','rule-field');
  const pathLabel=el('label','','Path');
  const pathId='rule-path-'+index;pathLabel.htmlFor=pathId;
  const pathGroup=el('div','path-match-inputs');
  const pathModeSelect=el('select');pathModeSelect.setAttribute('aria-label',`Rule ${index+1} path match type`);
  for(const [value,label] of [['prefix','Starts with'],['exact','Exact match']]){
   const option=el('option','',label);option.value=value;pathModeSelect.append(option);
  }
  pathModeSelect.value=draft.pathMode||'prefix';pathModeSelect.disabled=draft.matchAll;
  pathModeSelect.addEventListener('change',()=>{
   const existingPath=draft.match.pathExact||draft.match.pathPrefix||'';
   delete draft.match.pathExact;delete draft.match.pathPrefix;
   draft.pathMode=pathModeSelect.value;
   if(existingPath)draft.match[draft.pathMode==='exact'?'pathExact':'pathPrefix']=existingPath;
   markEdited();renderRuleEditor();
  });
  const pathInput=el('input');pathInput.type='text';pathInput.id=pathId;
  pathInput.placeholder='/callback';pathInput.disabled=draft.matchAll;
  pathInput.value=draft.match[draft.pathMode==='exact'?'pathExact':'pathPrefix']||'';
  pathInput.addEventListener('input',()=>{
   const pathKey=draft.pathMode==='exact'?'pathExact':'pathPrefix';
   draft.match[pathKey]=pathInput.value;markEdited();
  });
  pathGroup.append(pathModeSelect,pathInput);pathControl.append(pathLabel,pathGroup);basic.append(pathControl);
  condition.append(basic);
  const more=el('details','rule-extra');more.open=extraInputs.some(([,key])=>!!draft.match[key]);
  const summary=el('summary','','More match conditions (query, headers, body)');more.append(summary);
  const extraGrid=el('div','rule-fields');
  for(const [label,key,placeholder] of extraInputs){
   const control=ruleInput(label,draft.match[key]||'',v=>{draft.match[key]=v},{placeholder});
   control.querySelector('input').disabled=draft.matchAll;extraGrid.append(control);
  }
  more.append(extraGrid);condition.append(more);
  if(draft.matchAll)condition.append(el('p','rule-warning','This rule matches every request. Place it last so later rules can still run.'));
  card.append(condition);
  const result=el('div','rule-subsection');result.append(el('h4','','Then respond with'));
  const fields=el('div','rule-fields rule-response-fields');
  fields.append(ruleInput('HTTP status',draft.response.status,v=>draft.response.status=v,{type:'number',min:200,max:599}),ruleInput('Delay (ms)',draft.response.delayMs,v=>draft.response.delayMs=v,{type:'number',min:0,max:2000}));
  result.append(fields);
  const body=ruleInput('Response body (exact text)',draft.response.body,v=>draft.response.body=v,{multiline:true});result.append(body);
  const headerArea=el('div','rule-header-area');headerArea.append(el('div','rule-header-label','Response headers'));
  const rows=el('div','rule-header-rows');
  draft.headerRows.forEach((h,rowIndex)=>{
   const line=el('div','rule-header-row');const k=el('input'),v=el('input');
   k.type='text';k.placeholder='Header name';k.value=h.name;k.setAttribute('aria-label',`Rule ${index+1} response header name`);
   v.type='text';v.placeholder='Header value';v.value=h.value;v.setAttribute('aria-label',`Rule ${index+1} response header value`);
   k.addEventListener('input',()=>{h.name=k.value;markEdited()});v.addEventListener('input',()=>{h.value=v.value;markEdited()});
   const rm=button('×','icon-btn',()=>{draft.headerRows.splice(rowIndex,1);markEdited();renderRuleEditor()});rm.setAttribute('aria-label','Remove rule header');line.append(k,v,rm);rows.append(line);
  });
  headerArea.append(rows,button('+ Add response header','secondary-link',()=>{draft.headerRows.push({name:'',value:''});markEdited();renderRuleEditor()}));result.append(headerArea);
  card.append(result);visual.append(card);
 });
 const add=button('+ Add conditional rule','subtle add-rule',()=>{
  if(visualRules.length>=12){notify('Maximum of 12 conditional rules.');return}
  visualRules.push(toVisualRule({match:{method:'GET'},response:{status:200,headers:{},body:'',delayMs:0}}));markEdited();renderRuleEditor();
 });add.disabled=visualRules.length>=12;visual.append(add);
}
function moveRule(index,direction){
 const next=index+direction;if(next<0||next>=visualRules.length)return;
 [visualRules[index],visualRules[next]]=[visualRules[next],visualRules[index]];markEdited();renderRuleEditor();
}

async function saveSettings(token){
 try{
  const payload={response:{status:Number($('responseStatus').value),delayMs:Number($('responseDelay').value),headers:readHeaders(),body:$('responseBody').value},rules:readCurrentRules()};
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
