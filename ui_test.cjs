// Run with node --test ui_test.cjs. No browser or packages required.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function consoleUI(fetch) {
  const elements = new Map();
  const get = id => {
    if (!elements.has(id)) elements.set(id, {
      value: '', checked: false, disabled: false, style: {}, textContent: '', innerHTML: '',
      classList: { toggle() {}, remove() {} }, addEventListener() {}, focus() {},
    });
    return elements.get(id);
  };
  const context = vm.createContext({
    document: { getElementById:get, querySelectorAll:()=>[], querySelector:()=>null, addEventListener() {} },
    localStorage: { getItem:()=>null, setItem() {} },
    location: { origin:'http://localhost:8795' },
    navigator: { clipboard: { writeText:async () => {} } },
    Headers, URL, TextEncoder, AbortController, AbortSignal, performance, fetch,
    setTimeout, clearTimeout, setInterval:()=>0, clearInterval() {},
  });
  let source = fs.readFileSync('index.html', 'utf8').match(/<script>([\s\S]*?)<\/script>/)[1];
  source = source.replace(/\/\* ── Init ── \*\/[\s\S]*$/, '');
  vm.runInContext(source, context);
  vm.runInContext('refresh = async () => {}; savePrefs = () => {}; toast = (message) => { document.getElementById("toast").textContent = message; };', context);
  get('c-timeout').value = '15000';
  get('c-headers').value = '{}';
  return { get, run:code => vm.runInContext(code, context) };
}

test('scenario presets load and raw malformed bodies reach the server', async () => {
  let sent;
  const ui = consoleUI(async (url, options) => {
    sent = {url, ...options};
    return new Response('{"detail":"Invalid JSON"}', {status:400});
  });
  assert.equal(ui.run('scenarios.length'), 12);
  for (let i = 0; i < 12; i++) {
    ui.run(`loadScenario(${i})`);
    assert.ok(ui.get('c-path').value.startsWith('/'));
  }
  ui.run('loadScenario(8)');
  await ui.run('composeSend()');
  assert.equal(sent.body, '{"broken":');
  assert.equal(sent.method, 'POST');
  assert.equal(sent.headers.get('Content-Type'), 'application/json');
  assert.match(ui.run('lastResponse'), /PASS · expected 400/);
  ui.get('c-body').value = 'changed';
  ui.run('replayHistory(0)');
  assert.equal(ui.get('c-body').value, '{"broken":');
});

test('local-only requests and header validation prevent unintended requests', async () => {
  let calls = 0;
  const ui = consoleUI(async () => { calls++; return new Response('null'); });
  ui.get('c-method').value = 'GET';
  for (const path of ['https://example.com', '//example.com', '/\\example.com']) {
    ui.get('c-path').value = path;
    await ui.run('composeSend()');
  }
  assert.equal(calls, 0);
  ui.get('c-path').value = '/lab/echo';
  for (const headers of ['[]', 'null', '{"X-Test":42}', '{bad']) {
    ui.get('c-headers').value = headers;
    await ui.run('composeSend()');
  }
  assert.equal(calls, 0);
});

test('GET omits body; null and plain text remain visible and escaped', async () => {
  let sent;
  const ui = consoleUI(async (_url, options) => { sent = options; return new Response('null'); });
  ui.run('loadScenario(0)');
  ui.get('c-method').value = 'GET';
  await ui.run('composeSend()');
  assert.equal(sent.body, undefined);
  assert.match(ui.run('lastResponse'), /\nnull\n/);
  ui.run(`renderComposeOut({method:'GET',path:'/lab/echo',status:200,text:'<script>alert(1)</script>',errMsg:'',ms:1,responseHeaders:{}})`);
  assert.ok(!ui.get('compose-out').innerHTML.includes('<script>'));
  assert.ok(ui.get('compose-out').innerHTML.includes('&lt;script&gt;'));
});

test('timeouts and cancellation restore controls; duplicate sends are ignored', async () => {
  let calls = 0;
  const ui = consoleUI(async (_url, options) => {
    calls++;
    return new Promise((_resolve, reject) => options.signal.addEventListener('abort', () => reject(new Error('aborted'))));
  });
  ui.run('loadScenario(4)');
  ui.get('c-timeout').value = '100';
  const pending = ui.run('composeSend()');
  await ui.run('composeSend()');
  assert.equal(calls, 1);
  await pending;
  assert.match(ui.run('lastResponse'), /Timed out after 100 ms/);
  assert.equal(ui.get('c-send').disabled, false);
  assert.equal(ui.get('c-cancel').disabled, true);
  const cancelled = ui.run('composeSend()');
  ui.run('cancelCompose()');
  await cancelled;
  assert.match(ui.run('lastResponse'), /Request cancelled/);
});

test('every JSON value, including null and _error objects, can be submitted', async () => {
  let body;
  const ui = consoleUI(async (_url, options) => { body = options.body; return new Response('{"id":1}', {status:201}); });
  for (const value of ['null', 'false', '42', '"text"', '[1,2]', '{"_error":"payload data"}']) {
    ui.get('p-json').value = value;
    await ui.run('createPublic()');
    assert.equal(body, value);
  }
  assert.equal(ui.run('relativeTime(new Date().toISOString())'), 'just now');
});

test('upload form normalizes filenames and sends raw bytes with an API key', async () => {
  const requests = [];
  const ui = consoleUI(async (path, options) => {
    requests.push({path,options});
    if (path.includes('filename=')) return Response.json({filename:'hello-world.txt',size:5});
    if (path.endsWith('/status')) return Response.json({configured:true});
    return Response.json({files:[],next_cursor:''});
  });
  const file = {name:'hello world.txt',size:5,type:'text/plain'};
  ui.get('upload-file').files = [file];
  ui.get('upload-api-key').value = 'test-key';
  await ui.run('uploadFile()');
  assert.equal(requests[0].path,'/secure/uploads?filename=hello-world.txt');
  assert.equal(requests[0].options.body,file);
  assert.equal(requests[0].options.headers['X-API-Key'],'test-key');
  assert.equal(ui.get('upload-send').disabled,false);
  assert.match(ui.get('upload-status').textContent,/File storage ready/);
});

test('empty or oversized uploads do not send and unconfigured R2 gives clear feedback', async () => {
  let calls = 0;
  const ui = consoleUI(async () => { calls++; return Response.json({configured:false}); });
  for (const size of [0,10*1024*1024+1]) {
    ui.get('upload-file').files = [{name:'file.txt',size}];
    await ui.run('uploadFile()');
  }
  assert.equal(calls,0);
  ui.get('upload-api-key').value = 'test-key';
  await ui.run('refreshUploads()');
  assert.match(ui.get('upload-status').textContent,/File storage is not connected/);
});

test('one save form preserves public/private routing and sends the selected value', async () => {
  const sent = [];
  const ui = consoleUI(async (path, options) => {
    sent.push({path,options});
    return Response.json({id:1},{status:201});
  });
  ui.get('save-scope').value = 'public';
  ui.get('p-raw').value = 'An ordinary message';
  await ui.run('saveData()');
  assert.equal(sent[0].path,'/public/items');
  assert.equal(sent[0].options.body,'"An ordinary message"');
  ui.get('save-scope').value = 'secure';
  ui.get('api-key').value = 'test-key';
  ui.get('p-json').value = '{"nested":{"ok":true}}';
  await ui.run('saveData()');
  assert.equal(sent[1].path,'/secure/items');
  assert.equal(sent[1].options.headers['X-API-Key'],'test-key');
  assert.equal(sent[1].options.body,'{"nested":{"ok":true}}');
  assert.equal(ui.get('p-json').value,'');
  assert.equal(ui.get('save-data').disabled,false);
});

test('private saves keep the message when a key is missing; failed keys give clear feedback', async () => {
  const ui = consoleUI(async () => Response.json({detail:'Invalid key'},{status:401}));
  ui.get('save-scope').value = 'secure';
  ui.get('p-raw').value = 'Keep this message';
  await ui.run('saveData()');
  assert.equal(ui.get('p-raw').value,'Keep this message');
  assert.equal(ui.get('save-data').disabled,false);
  ui.get('api-key').value = 'wrong-key';
  await ui.run('applyAccessKey()');
  assert.match(ui.get('access-status').textContent,/did not work/);
});

test('editing keeps JSON types and ordinary messages instead of encoding everything as a string', async () => {
  let sent;
  const ui = consoleUI(async (_path, options) => { sent = options.body; return Response.json({id:1}); });
  for (const [original, edited, expected] of [
    [{value:1},'{"value":2}','{"value":2}'],
    [false,'true','true'],
    [1,'2','2'],
    ['old message','new message','"new message"'],
  ]) {
    ui.run('allItems = [{id:1,scope:"public",payload:'+JSON.stringify(original)+'}]');
    const box = ui.get('payload-1');
    box.dataset = {editing:'1'};
    box.textContent = edited;
    box.closest = () => ({querySelector:() => ({textContent:''})});
    await ui.run('editItem(1)');
    assert.equal(sent,expected);
  }
});
