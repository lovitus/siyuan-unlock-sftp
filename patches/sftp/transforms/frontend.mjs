import ts from 'typescript';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

export function transform(text, kind) {
    const source = ts.createSourceFile('source.ts', text, ts.ScriptTarget.Latest, true);
    const find = (root, predicate) => { const found=[]; const walk=n=>{if(predicate(n))found.push(n);ts.forEachChild(n,walk);};walk(root);return found; };
    const one = nodes => {if(nodes.length!==1)throw new Error(`expected one ${kind} anchor, found ${nodes.length}`);return nodes[0];};
    const edits=[];
    const replace=(node,value)=>{
        let start=node.getStart(source);
        const line=text.lastIndexOf('\n',start-1)+1;
        if(value==='' && /^\s*$/.test(text.slice(line,start))) start=line;
        edits.push([start,node.end,value]);
    };
    const insert=(pos,value)=>edits.push([pos,pos,value]);
    const named = name => one(find(source,n=>ts.isVariableDeclaration(n)&&n.name.getText(source)===name));
    const str=(n,value)=>ts.isStringLiteral(n)&&n.text===value;
    if (kind==='syncTab') {
        const select=one(find(named('registerSyncGroup'),n=>ts.isCallExpression(n)&&str(n.arguments[0]??{},'sync.provider')));
        const options=one(find(select,n=>ts.isPropertyAssignment(n)&&n.name.getText(source)==='options')).initializer;
        if(!ts.isArrayLiteralExpression(options))throw new Error('provider options are no longer an array');
        insert(options.elements.pos,'{value: 5, label: "SFTP"},');
        const registration=one(find(named('registerSyncTab'),n=>ts.isExpressionStatement(n)&&ts.isCallExpression(n.expression)&&n.expression.expression.getText(source)==='registerAccountGroup'));
        replace(registration,'');
        const accountImport=one(find(source,n=>ts.isImportDeclaration(n)&&n.moduleSpecifier.text==='./accountUi'));
        replace(accountImport,'');
    } else if(kind==='syncUi') {
        const render=named('renderProviderConfig').initializer;
        if(!ts.isArrowFunction(render)||render.parameters.length!==1||!ts.isBlock(render.body))throw new Error('renderer contract changed');
        insert(0,'import {renderSFTPConfig} from "./sftp";\n');
        insert(render.body.getStart(source)+1,`\nif (renderSFTPConfig(${render.parameters[0].name.getText(source)})) { return; }\n`);
    } else if(kind==='topBar') {
        const branch=one(find(source,n=>ts.isIfStatement(n)&&find(n.expression,x=>str(x,'toolbarVIP')).length===1&&find(n.expression,x=>str(x,'toolbarTitle')).length===1));
        const open=one(find(branch.thenStatement,n=>ts.isExpressionStatement(n)&&ts.isCallExpression(n.expression)&&n.expression.expression.getText(source)==='openSetting'));
        replace(open,''); // Preserve surrounding menu, event handling and imports used elsewhere.
    } else if(kind==='types') {
        const sync=one(find(source,n=>ts.isInterfaceDeclaration(n)&&n.name.text==='ISync'));
        insert(sync.members.pos,'\nsftp?: {host: string; port: number; username: string; password: string; path: string; hostKey: string; timeout: number; concurrentReqs: number};\n');
    } else throw new Error('unknown transform');
    edits.sort((a,b)=>b[0]-a[0]);let last=text.length;
    for(const [start,end,value] of edits){if(end>last)throw new Error('overlapping edits');text=text.slice(0,start)+value+text.slice(end);last=start;}
    const parsed=ts.createSourceFile('result.ts',text,ts.ScriptTarget.Latest,true);
    if(parsed.parseDiagnostics.length)throw new Error('invalid generated TypeScript');
    return text;
}
if(process.argv[1]===fileURLToPath(import.meta.url)) {
    try {
        for(const [file,kind] of [['config/tabs/syncTab.ts','syncTab'],['config/tabs/syncUi.ts','syncUi'],['layout/topBar.ts','topBar'],['types/config.d.ts','types']]) {
            const full=path.join(process.argv[2],'app/src',file);
            fs.writeFileSync(full,transform(fs.readFileSync(full,'utf8'),kind));
        }
    } catch(error) { console.error('PATCH_CONFLICT:',error.message);process.exit(42); }
}
