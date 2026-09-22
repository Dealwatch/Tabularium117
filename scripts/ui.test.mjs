// Dependency-free DOM logic tests. These do not render CSS or replace browser QA.
// Run: node --test scripts/ui.test.mjs (Node 22+).
import { test } from 'node:test';
import assert from 'node:assert/strict';

class Element extends EventTarget {
  constructor(tag = '') { super(); this.tagName = tag; this.children = []; this.attributes = new Map(); this.style = {}; this.className = ''; }
  append(...items) {
    for (let node of items) {
      if (typeof node === 'string') node = new Text(node);
      if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1);
      node.parent = this; this.children.push(node);
    }
  }
  replaceChildren(...items) { for (const n of this.children) n.parent = null; this.children = []; this.append(...items); }
  get firstChild() { return this.children[0]; }
  get childElementCount() { return this.children.length; }
  set textContent(value) { this.replaceChildren(new Text(String(value))); }
  get textContent() { return this.children.map(n => n.textContent).join(''); }
  setAttribute(k, v) { this.attributes.set(k, String(v)); }
  getAttribute(k) { return this.attributes.get(k) ?? null; }
  removeAttribute(k) { this.attributes.delete(k); }
  querySelectorAll(tag) { return this.children.flatMap(n => [...(n.tagName === tag ? [n] : []), ...n.querySelectorAll(tag)]); }
  focus() { document.activeElement = this; }
  click() { this.dispatchEvent(new Event('click')); }
  get classList() { return { add: c => { this.className += ' ' + c; } }; }
}
class Text extends Element {
  constructor(value) { super('#text'); this.value = value; }
  get textContent() { return this.value; }
  set textContent(v) { this.value = v; }
}
const doc = new Element('document');
doc.createElement = tag => new Element(tag);
doc.createTextNode = value => new Text(value);
globalThis.document = doc;
globalThis.window = { location: { hash: '', origin: 'http://localhost' } };
globalThis.localStorage = { getItem: () => null };
const { i18n } = await import('../web/js/i18n.js');
const { api } = await import('../web/js/api.js');
const { renderIslandDetail } = await import('../web/js/views/islands.js');
const { renderEfficiency } = await import('../web/js/views/efficiency.js');
const { islandNavigation } = await import('../web/js/views/navigation.js');
const island = { id: '3245-1', name: 'Juliana', sessionName: 'Latium', products: 3, deficits: 1 };
const products = [
  { guid: 1, name: 'Bread', generation: 5, consumption: 3, delta: 2, buildings: 2 },
  { guid: 2, name: 'Wheat', generation: 3, consumption: 7, delta: -4, buildings: 3 },
  { guid: 3, name: 'Fish', generation: 1, consumption: 1, delta: 0, buildings: 1 },
];
const state = () => ({ islands: [island], alerts: [{ islandId: island.id, productGuid: 1, rule: 'productivity_drop', detail: 'test' }], status: {} });
const nodes = (root, tag) => root.querySelectorAll(tag);
const button = (root, text) => nodes(root, 'button').find(n => n.textContent === text);
const bodyText = root => nodes(root, 'tbody')[0].textContent;
const tick = () => new Promise(resolve => setImmediate(resolve));

// Sequential tests share only a stub API; production modules remain unmodified.
test('filters intersect with search; zero results are explained; warning scope is per island', async () => {
  i18n.lang = 'en'; api.products = async () => ({ island, products });
  const root = new Element(); const store = state();
  store.alerts.push({ islandId: 'other', productGuid: 2, rule: 'deficit' });
  const cleanup = await renderIslandDetail(root, island.id, store);
  assert.match(bodyText(root), /\+2\.0/);
  button(root, 'Deficits').click(); assert.match(bodyText(root), /Wheat/); assert.doesNotMatch(bodyText(root), /Bread/);
  button(root, 'Warnings').click(); assert.match(bodyText(root), /Bread/); assert.doesNotMatch(bodyText(root), /Wheat/);
  const input = nodes(root, 'input')[0]; input.value = 'missing'; input.dispatchEvent(new Event('input'));
  assert.match(bodyText(root), /No matching goods/);
  input.value = 'bread'; input.dispatchEvent(new Event('input')); assert.match(bodyText(root), /Bread/);
  const link = nodes(root, 'a').find(n => n.className === 'product-link');
  assert.equal(link.href, '#/island/3245-1/product/1'); assert.match(link.getAttribute('aria-label'), /Open history/);
  cleanup();
});
test('sorting exposes direction and preserves keyboard focus', async () => {
  api.products = async () => ({ island, products }); const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, state());
  assert.equal(nodes(root, 'tbody')[0].children[0].children[0].textContent, 'Wheat ↗');
  button(root, 'Balance/min ↑').click();
  assert.match(nodes(root, 'tbody')[0].children[0].children[0].textContent, /Bread/);
  assert.equal(nodes(root, 'th')[3].getAttribute('aria-sort'), 'descending');
  assert.equal(document.activeElement, nodes(root, 'thead')[0].querySelectorAll('button')[3]); cleanup();
});
test('German units, decimal comma and warm-up are explicit', async () => {
  i18n.lang = 'de'; api.products = async () => ({ island, products }); const root = new Element();
  let cleanup = await renderIslandDetail(root, island.id, state());
  assert.match(root.textContent, /Bilanz\/min/); assert.match(bodyText(root), /\+2,0/); cleanup();
  api.products = async () => ({ island, products: [] });
  cleanup = await renderIslandDetail(new Element(), island.id, state()); cleanup();
  const waiting = new Element(); const store = state(); store.status.warmingUp = true;
  cleanup = await renderIslandDetail(waiting, island.id, store);
  assert.match(waiting.textContent, /Warte auf den ersten Statistik-Tick/); cleanup(); i18n.lang = 'en';
});
test('island switch preserves efficiency route and safely renders user names', () => {
  const store = state(); store.islands.push({ ...island, id: '6627-2', name: '<script>test</script>' });
  const nav = islandNavigation(island.id, store, 'efficiency');
  assert.equal(nodes(nav, 'script').length, 0);
  const select = nodes(nav, 'select')[0]; select.value = '6627-2'; select.dispatchEvent(new Event('change'));
  assert.equal(window.location.hash, '#/island/6627-2/efficiency');
  assert.equal(nodes(nav, 'a')[1].getAttribute('aria-current'), 'page');
});
test('late and out-of-order product responses do not overwrite the latest data', async () => {
  api.products = async () => ({ island, products }); const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, state());
  const pending = []; api.products = () => new Promise(resolve => pending.push(resolve));
  const fire = () => document.dispatchEvent(new CustomEvent('tabularium-snapshot', { detail: { id: island.id } }));
  fire(); fire();
  pending[1]({ island, products: [{ ...products[0], name: 'Newest' }] }); await tick();
  pending[0]({ island, products: [{ ...products[0], name: 'Old' }] }); await tick();
  assert.match(bodyText(root), /Newest/); assert.doesNotMatch(bodyText(root), /Old/);
  fire(); cleanup(); pending[2]({ island, products: [] }); await tick();
  assert.match(bodyText(root), /Newest/);
});
test('efficiency preserves zero-baseline and boosted values', async () => {
  api.efficiency = async () => ({ island, products: [
    { name: 'Boosted', generation: 15, perfectGeneration: 10, efficiency: 1.5, avgProductivity: 150, wasted: -5 },
    { name: 'Unknown', generation: 0, perfectGeneration: 0, efficiency: null, avgProductivity: 0, wasted: 0 },
  ] });
  const root = new Element(); const cleanup = await renderEfficiency(root, island.id, state());
  assert.match(bodyText(root), /150%/); assert.equal(nodes(root, 'tbody')[0].children[1].className, 'no-data'); cleanup();
});

test('region grouping preserves session identity, unknown regions and interleaved islands', async () => {
  const { groupIslands } = await import('../web/js/regions.js');
  const islands = [
    { ...island, sessionGuid: 1 },
    { ...island, id: '2-1', sessionGuid: 2, sessionName: 'Albion' },
    { ...island, id: '1-2', sessionGuid: 1 },
    { ...island, id: '3-1', sessionGuid: 3, sessionName: '' },
  ];
  const groups = groupIslands(islands);
  assert.deepEqual(groups.map(g => g.name), ['Latium', 'Albion', '#3']);
  assert.deepEqual(groups[0].islands.map(i => i.id), [island.id, '1-2']);
  const nav = islandNavigation(island.id, { islands }, 'goods');
  assert.deepEqual(nodes(nav, 'optgroup').map(g => g.label), ['Latium', 'Albion', '#3']);
  assert.equal(nodes(nav, 'option').length, 4);
});
test('overview links open an island with the deficit filter; warm-up avoids false zero balances', async () => {
  const { renderRegionOverview } = await import('../web/js/views/overview.js');
  i18n.lang = 'en'; const store = state();
  const overview = renderRegionOverview(store);
  const link = nodes(overview, 'a').find(a => a.href.endsWith('?filter=deficits'));
  assert.equal(link.href, '#/island/3245-1?filter=deficits');
  assert.match(link.getAttribute('aria-label'), /Juliana/);
  api.products = async () => ({ island, products });
  const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, store, 'deficits');
  assert.match(bodyText(root), /Wheat/); assert.doesNotMatch(bodyText(root), /Bread/);
  assert.equal(button(root, 'Deficits').getAttribute('aria-pressed'), 'true'); cleanup();
  store.status.warmingUp = true;
  const waiting = renderRegionOverview(store);
  assert.equal(nodes(waiting, 'a').length, 1);
  assert.match(waiting.textContent, /waiting for statistics/);
});
