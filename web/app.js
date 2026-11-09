'use strict';
let adminToken='';
let selected='';
let selectedEvent='';
let editing=false;
const $=id=>document.getElementById(id);
const el=(tag,cls,text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=text;return n};
const no=(message)=>{$('notice').textContent=message;};
function button(label,cls,click){const b=el('button',cls,label);b.type='button';b.addEventListener('click',click);return b}
function fmtDate(v){return new Date(v).toLocaleString()}
function remaining(v){let sec=Math.max(0,Math.floor((new Date(v).getTime()-Date.now())/1000));return sec>60?Math.ceil(sec/60)+' min left':sec+' sec left'}
async function api(path,options={}){
 const headers={'Authorization':'Bearer '+adminToken};
 if(options.body!==undefined)headers['Content-Type']='application/json';
 const res=await fetch('/api/'+path,{...options,headers,cache:'no-store'});
 const data=await res.json();if(!res.ok)throw new Error(data.error||res.statusText);return data;
}
function login(){adminToken=$('adminToken').value.trim();$('adminToken').value=''; if(!adminToken)return;load().then(()=>{$('loginPane').classList.add('hidden');$('workspace').classList.remove('hidden')}).catch(e=>{adminToken='';alert('Authentication failed: '+e.message)})}
$('unlock').addEventListener('click',login);
$('adminToken').addEventListener('keydown',e=>{if(e.key==='Enter')login()});
$('lock').addEventListener('click',()=>{adminToken='';selected='';$('workspace').classList.add('hidden');$('loginPane').classList.remove('hidden');$('sessionPane').replaceChildren(el('p','muted','Select a session.'))});
$('refresh').addEventListener('click',()=>load().catch(e=>no(e.message)));
$('create').addEventListener('click',async()=>{
 try{
  const session=await api('endpoints',{method:'POST',body:JSON.stringify({name:$('name').value,ttlMinutes:Number($('ttl').value),maxRequests:Number($('requestLimit').value),bodyLimitKiB:Number($('bodyLimit').value)})});
  selected=session.token;selectedEvent='';editing=false;no('Endpoint created. Copy the capture URL to your authorized CTF challenge.');await load();
 }catch(e){no(e.message)}
});
async function load(){if(!adminToken)return;
 const all=await api('endpoints');const list=$('sessionList');list.replaceChildren();
 if(all.length===0){list.append(el('p','muted','No endpoints yet.'));selected='';$('sessionPane').replaceChildren(el('p','muted','Create an endpoint to begin.'));return}
 if(!selected||!all.some(s=>s.token===selected))selected=all[0].token;
 for(const s of all){const b=el('button','sessionbtn'+(selected===s.token?' active':''));b.type='button';b.append(el('strong','',s.name),el('div','smallline',`${s.seen}/${s.maxRequests} requests · ${remaining(s.expiresAt)}`));b.addEventListener('click',()=>{selected=s.token;selectedEvent='';editing=false;load().catch(e=>no(e.message))});list.append(b)}
 const data=await api('endpoints/'+selected);renderSession(data);
}
function textField(label,value,id,rows){const l=el('label');l.textContent=label;const field=el(rows?'textarea':'input');field.id=id;if(!rows)field.type='text';field.value=value;l.append(field);return l}
function renderSession(s){
 const root=$('sessionPane');root.replaceChildren();
 const h=el('div','detailhead');h.append(el('h2','',s.name),el('span','pill',remaining(s.expiresAt)));root.append(h);
 root.append(el('div','meta',`${s.seen}/${s.maxRequests} requests received · ${s.captures.length} retained · Body cap: ${s.bodyLimitKiB} KiB`));
 const urlrow=el('div','endpointurl');urlrow.append(el('code','',s.url),button('Copy URL','ghost compact',()=>navigator.clipboard.writeText(s.url).then(()=>no('Capture URL copied.')).catch(()=>no('Copy failed: select the URL manually.'))));root.append(urlrow);
 const toolbar=el('div','toolbar');toolbar.append(button('Export JSON','ghost compact',()=>exportSession(s.token)),button('Delete now','danger compact',()=>deleteSession(s.token)));root.append(toolbar);
 root.append(el('div','divider'));
 root.append(el('h2','','Response configuration'));
 root.append(el('p','hint','Changes affect future requests. The first matching conditional rule wins; otherwise the default response is sent.'));
 const settings=el('div','settings');
 const top=el('div','twocol');top.append(textField('HTTP status',String(s.response.status),'responseStatus',false),textField('Delay in milliseconds (0–2000)',String(s.response.delayMs),'responseDelay',false));settings.append(top);
 settings.append(textField('Response headers (JSON object)',JSON.stringify(s.response.headers||{},null,2),'responseHeaders',true));
 settings.append(textField('Response body (exact text)',s.response.body||'','responseBody',true));
 settings.append(el('h3','','Conditional response rules (JSON array)'));
 settings.append(el('p','hint','Example: [{"match":{"method":"GET","queryKey":"role","queryValue":"admin"},"response":{"status":200,"headers":{"Content-Type":"application/json"},"body":"{\\"admin\\":true}","delayMs":0}}]'));
 settings.append(textField('Rules: first match takes priority',JSON.stringify(s.rules||[],null,2),'rulesJSON',true));
 settings.append(button('Save response settings','',()=>saveSettings(s.token)));
 root.append(settings);
 root.append(el('div','divider'));
 const headline=el('div','sectionhead');headline.append(el('h2','','Captured requests'),el('span','muted',String(s.captures.length)+' shown'));root.append(headline);
 const panels=el('div','events');const list=el('div','eventlist');const detail=el('div','eventpanel');
 if(s.captures.length===0){list.append(el('p','muted','Waiting for requests...'));detail.append(el('p','muted','Send an HTTP request to the URL above.'))}
 else{
  const reverse=[...s.captures].reverse();if(!selectedEvent||!reverse.some(e=>e.id===selectedEvent))selectedEvent=reverse[0].id;
  for(const event of reverse){const b=el('button','event'+(selectedEvent===event.id?' active':''),`${event.method} ${event.path||'/'} · ${fmtDate(event.at)}`);b.type='button';b.addEventListener('click',()=>{selectedEvent=event.id;renderSession(s)});list.append(b)}
  const ev=reverse.find(e=>e.id===selectedEvent);if(ev){const shown={...ev,body:ev.bodyEncoding==='base64'?ev.body+' (base64 encoded)':ev.body};detail.append(el('pre','',JSON.stringify(shown,null,2)))}
 }
 panels.append(list,detail);root.append(panels);
}
async function saveSettings(token){
 try{
  const payload={response:{status:Number($('responseStatus').value),delayMs:Number($('responseDelay').value),headers:JSON.parse($('responseHeaders').value),body:$('responseBody').value},rules:JSON.parse($('rulesJSON').value)};
  await api('endpoints/'+token,{method:'PUT',body:JSON.stringify(payload)});no('Response configuration saved.');await load();
 }catch(e){no('Could not save: '+e.message)}
}
async function deleteSession(token){if(!confirm('Destroy this endpoint and all in-memory captures?'))return;
 try{await api('endpoints/'+token,{method:'DELETE'});selected='';selectedEvent='';no('Endpoint destroyed.');await load()}catch(e){no(e.message)}
}
async function exportSession(token){try{
 const data=await api('endpoints/'+token+'/export');const blob=new Blob([JSON.stringify(data,null,2)],{type:'application/json'});const u=URL.createObjectURL(blob);const anchor=el('a');anchor.href=u;anchor.download='hookloom-'+token.slice(0,8)+'.json';document.body.append(anchor);anchor.click();anchor.remove();setTimeout(()=>URL.revokeObjectURL(u),1000);no('Export downloaded; handle CTF secrets carefully.');
 }catch(e){no(e.message)}}
setInterval(()=>{if(adminToken && !$('workspace').classList.contains('hidden') && document.activeElement.tagName!=='TEXTAREA' && document.activeElement.tagName!=='INPUT' && document.activeElement.tagName!=='SELECT'){load().catch(e=>no(e.message))}},5000);
