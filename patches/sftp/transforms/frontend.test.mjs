import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import vm from 'node:vm';
import {JSDOM} from 'jsdom';
import ts from 'typescript';
import {transform} from './frontend.mjs';

const root = process.argv[2];
const dom = new JSDOM('<div id="root"><div id="syncProviderConfig"></div><div id="syncProviderConfigRow"></div></div>');
const {window} = dom;
window.siyuan = {config: {readonly:false, system:{container:'docker'}, sync:{provider:5, sftp:null}}, languages:{save:'Save', _kernel:{}}};
const calls=[];
let response, resolveSave;
const post = (url,data)=>{calls.push({url,data});return response ?? Promise.resolve({code:0,data});};
const common = new Proxy({fetchSyncPost:post, isPaidUser:()=>true, needSubscribe:()=>false,
    getHostCapabilities:()=>({importExport:false}), getCloudURL:()=>'', escapeHtmlTextAndAttr:x=>x},
    {get:(obj,name)=>obj[name]??(()=>{})});
function load(source, modules={}) {
    const exports={};
    const js=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2021,module:ts.ModuleKind.CommonJS}}).outputText;
    vm.runInNewContext(js,{exports,require:name=>modules[name]??common,window,document:window.document,console, setTimeout,clearTimeout,Error});
    return exports;
}
const sftp=load(readFileSync(new URL('../../../overlays/app/src/config/tabs/sftp.ts',import.meta.url),'utf8'));
let render=sftp.renderSFTPConfig;
if(root) {
    const ui=readFileSync(resolve(root,'app/src/config/tabs/syncUi.ts'),'utf8');
    const patched=ui.includes('import {renderSFTPConfig}') ? ui : transform(ui,'syncUi');
    render=load(patched+'\nexport {renderProviderConfig};',{'./sftp':sftp}).renderProviderConfig;
    const groups=[], selections=[];
    const group=new Proxy({}, {get:(_,name)=>(...args)=>{if(name==='select') selections.push(args);}});
    const source=readFileSync(resolve(root,'app/src/config/tabs/syncTab.ts'),'utf8');
    load(source).registerSyncTab({group:id=>{groups.push(id);return group;}});
    assert.deepEqual(groups,['sync','repo']);
    assert(selections.find(([key])=>key==='sync.provider')[1].options.some(o=>o.value===5&&o.label==='SFTP'));
}
const panel=window.document.querySelector('#root');
render(panel);
assert.equal(panel.querySelectorAll('input').length,8);
const field=name=>panel.querySelector(`[name="${name}"]`);
Object.entries({host:' localhost ',username:' alice ',password:' p<&> ',path:'/notes',hostKey:'SHA256:'+'A'.repeat(43)}).forEach(([k,v])=>field(k).value=v);
const submit=()=>panel.querySelector('form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));
const settle=()=>new Promise(r=>setImmediate(r));
field('path').value='relative';submit();assert.equal(calls.length,0);
field('path').value='/notes';field('port').value='0';submit();assert.equal(calls.length,0);
field('port').value='22';response=new Promise(r=>resolveSave=r);submit();submit();
assert.equal(calls.length,1,'double click must save once');
assert.equal(calls[0].url,'/api/sync/setSyncProviderSFTP');
assert.equal(calls[0].data.sftp.host,'localhost');
assert.equal(calls[0].data.sftp.port,22);
assert.equal(calls[0].data.sftp.password,' p<&> ');
resolveSave({code:0,data:calls[0].data});await settle();
assert.equal(window.siyuan.config.sync.sftp.path,'/notes');
assert.equal(panel.querySelector('[role=status]').textContent,'Saved');
render(panel);assert.equal(field('password').value,' p<&> ');
response=Promise.resolve({code:-1,msg:'save failed'});field('path').value='/different';submit();await settle();
assert.equal(window.siyuan.config.sync.sftp.path,'/notes','failed save cannot change local configuration');
assert.equal(panel.querySelector('[role=status]').textContent,'save failed');
window.siyuan.config.readonly=true;render(panel);assert(field('host').disabled);submit();assert.equal(calls.length,2);
window.siyuan.config.readonly=false;
window.siyuan.config.sync={...window.siyuan.config.sync,provider:3};submit();assert.equal(calls.length,2,'stale detached form cannot save after switching providers');
assert.equal(sftp.renderSFTPConfig(panel),false);
// Layout and unrelated field edits do not change the structural hook.
const original='const renderProviderConfig = (container: Element) => { const newUpstreamCode = 42; return newUpstreamCode; };';
assert.match(transform(original,'syncUi'),/renderSFTPConfig\(container\)/);
assert.match(transform(original,'syncUi'),/const newUpstreamCode = 42/);
assert.throws(()=>transform('const renamed = () => {};','syncUi'),/anchor/);
console.log('PASS: real provider registration/rendering, save/readback, validation, errors, read-only and provider switching');
