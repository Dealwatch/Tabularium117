// Logic tests for the browser UI, without a browser and without a
// dependency: node --test scripts/ui.test.mjs (Node 22 or newer).
//
// The frontend has no build step and therefore no test runner either, which
// left its logic - filters, sorting, the warm-up state, which of two answers
// wins - checkable only by hand or in a full browser.
// This file gives the view modules just enough DOM to run against, so the
// parts that are decisions rather than pixels can be tested in a second.
//
// What it deliberately does not do: render, lay out, or style anything. A
// green run here says the logic holds, not that the page looks right.
// `docs/beta.md` is still what covers the looking.
import { test } from "node:test";
import assert from "node:assert/strict";

// --- the smallest DOM these modules need -----------------------------------

class Element extends EventTarget {
  constructor(tag = "") {
    super();
    this.tagName = tag;
    this.children = [];
    this.attributes = new Map();
    this.style = {};
    this.className = "";
    this.parent = null;
  }
  append(...items) {
    for (let node of items) {
      if (typeof node === "string") node = new Text(node);
      if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1);
      node.parent = this;
      this.children.push(node);
    }
  }
  replaceChildren(...items) {
    for (const child of this.children) child.parent = null;
    this.children = [];
    this.append(...items);
  }
  get firstChild() {
    return this.children[0];
  }
  get childElementCount() {
    return this.children.length;
  }
  set textContent(value) {
    this.replaceChildren(new Text(String(value)));
  }
  get textContent() {
    return this.children.map((child) => child.textContent).join("");
  }
  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }
  getAttribute(name) {
    return this.attributes.get(name) ?? null;
  }
  removeAttribute(name) {
    this.attributes.delete(name);
  }
  hasAttribute(name) {
    return this.attributes.has(name);
  }
  // In a browser `element.title = x` and setAttribute("title", x) are the
  // same thing. The views use the property, the tests read the attribute,
  // and a double that does not reflect it would fail them both wrongly.
  get title() {
    return this.getAttribute("title") ?? "";
  }
  set title(value) {
    this.setAttribute("title", value);
  }
  querySelector(selector) {
    return this.querySelectorAll(selector)[0] ?? null;
  }
  // Enough of a selector engine for "tag" and ".class".
  querySelectorAll(selector) {
    const matches = (node) => (selector.startsWith(".")
      ? String(node.className).split(/\s+/).includes(selector.slice(1))
      : node.tagName === selector);
    return this.children.flatMap((node) => [
      ...(matches(node) ? [node] : []),
      ...node.querySelectorAll(selector),
    ]);
  }
  focus() {
    document.activeElement = this;
  }
  click() {
    this.dispatchEvent(new Event("click"));
  }
  get classList() {
    return {
      add: (name) => {
        this.className = `${this.className} ${name}`.trim();
      },
    };
  }
}

class Text extends Element {
  constructor(value) {
    super("#text");
    this.value = value;
  }
  get textContent() {
    return this.value;
  }
  set textContent(value) {
    this.value = value;
  }
}

const document_ = new Element("document");
document_.createElement = (tag) => new Element(tag);
document_.createTextNode = (value) => new Text(value);
globalThis.document = document_;
globalThis.window = { location: { hash: "" }, matchMedia: () => ({ matches: false }) };
globalThis.localStorage = { getItem: () => null, setItem: () => {} };

const { i18n } = await import("../web/js/i18n.js");
const { api } = await import("../web/js/api.js");
const { renderIslandDetail } = await import("../web/js/views/islands.js");
const { renderEfficiency } = await import("../web/js/views/efficiency.js");
const { renderAlerts } = await import("../web/js/views/alerts.js");

// --- fixtures --------------------------------------------------------------

const island = { id: "3245-1", name: "Juliana", sessionName: "Latium", products: 3, deficits: 1 };

const products = [
  { guid: 1, name: "Bread", generation: 5, consumption: 3, delta: 2, buildings: 2 },
  { guid: 2, name: "Wheat", generation: 3, consumption: 7, delta: -4, buildings: 3 },
  { guid: 3, name: "Fish", generation: 1, consumption: 1, delta: 0, buildings: 1 },
];

// One warning, on Bread, so that "warnings" and "deficits" select different
// rows - a filter that happened to select the same rows as another would
// prove nothing.
const storeWith = (...alerts) => ({
  islands: [island],
  alerts: [{ islandId: island.id, productGuid: 1, rule: "productivity_drop", detail: "test" }, ...alerts],
  status: {},
});

const nodes = (root, selector) => root.querySelectorAll(selector);
const rowText = (root) => nodes(root, "tbody")[0].textContent;
const button = (root, label) => nodes(root, "button").find((node) => node.textContent === label);

test("the filters intersect with the search, and an empty result says why", async () => {
  i18n.lang = "en";
  api.products = async () => ({ island, products });
  const root = new Element();
  // A warning on another island must not leak into this one's filter.
  const store = storeWith({ islandId: "9999-9", productGuid: 2, rule: "deficit", detail: "elsewhere" });
  const cleanup = await renderIslandDetail(root, island.id, store);

  assert.match(rowText(root), /Bread/);
  button(root, "Deficits").click();
  assert.match(rowText(root), /Wheat/);
  assert.doesNotMatch(rowText(root), /Bread/);

  button(root, "Warnings").click();
  assert.match(rowText(root), /Bread/);
  assert.doesNotMatch(rowText(root), /Wheat/, "the warning on the other island must not select its row here");

  const search = nodes(root, "input")[0];
  search.value = "wheat";
  search.dispatchEvent(new Event("input"));
  assert.match(rowText(root), /No matching goods/, "a filter and a search that exclude each other must explain the empty table");

  button(root, "All").click();
  assert.match(rowText(root), /Wheat/);
  cleanup();
});

test("sorting says which way it sorted and keeps the keyboard focus", async () => {
  i18n.lang = "en";
  api.products = async () => ({ island, products });
  const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, storeWith());

  // The default is the deficit first: that is what the table is for.
  assert.match(nodes(root, "tbody")[0].children[0].textContent, /Wheat/);
  assert.equal(nodes(root, "th")[3].getAttribute("aria-sort"), "ascending");

  const deltaHeader = nodes(root, "thead")[0].querySelectorAll("button")[3];
  deltaHeader.click();
  assert.match(nodes(root, "tbody")[0].children[0].textContent, /Bread/, "a second click reverses the order");
  assert.equal(nodes(root, "th")[3].getAttribute("aria-sort"), "descending");
  assert.equal(
    document.activeElement,
    nodes(root, "thead")[0].querySelectorAll("button")[3],
    "sorting replaces the header row, and the focus has to survive it",
  );
  cleanup();
});

test("a surplus carries its sign, and the name is a link into the history", async () => {
  i18n.lang = "en";
  api.products = async () => ({ island, products });
  const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, storeWith());

  assert.match(rowText(root), /\+2\.0/, "a positive balance is written +2.0, not 2.0");
  assert.match(rowText(root), /-4\.0/);

  // The table opens on the deficit, so the first link is Wheat's; Bread is
  // the row with the surplus this test is about.
  const link = nodes(root, ".product-link").find((node) => node.textContent.includes("Bread"));
  assert.equal(link.href, `#/island/${island.id}/product/1`);
  assert.match(link.getAttribute("aria-label"), /Open history/);
  cleanup();
});

test("an answer that was overtaken does not paint over the newer one", async () => {
  i18n.lang = "en";
  const tick = () => new Promise((resolve) => setImmediate(resolve));
  const row = (name) => ({ guid: 9, name, generation: 0, consumption: 0, delta: 0, buildings: 0 });

  api.products = async () => ({ island, products });
  const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, storeWith());

  // A snapshot starts a load that then hangs: this is the answer the user
  // has already stopped waiting for.
  let release;
  const slow = new Promise((resolve) => { release = resolve; });
  api.products = async () => { await slow; return { island, products: [row("Stale")] }; };
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: island.id } }));
  await tick();

  // The next snapshot answers immediately, and is the current picture.
  api.products = async () => ({ island, products: [row("Fresh")] });
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: island.id } }));
  await tick();
  assert.match(rowText(root), /Fresh/);

  release();
  await tick();
  assert.match(rowText(root), /Fresh/, "the newer answer stays");
  assert.doesNotMatch(rowText(root), /Stale/, "the overtaken answer must not win by arriving last");
  cleanup();
});

test("the efficiency view separates the two ways of producing nothing", async () => {
  i18n.lang = "en";
  api.efficiency = async () => ({
    island,
    products: [
      // Buildings that stand still: there is a potential, nothing comes out.
      { guid: 1, name: "Timber", generation: 0, perfectGeneration: 10, buildings: 2, efficiency: 0, wasted: 10, avgProductivity: 0 },
      // No buildings at all: nothing to compare against.
      { guid: 2, name: "Amphorae", generation: 0, perfectGeneration: 0, buildings: 0, efficiency: null, wasted: 0, avgProductivity: 0 },
      // A zero perfect value alone does not prove there are no buildings.
      { guid: 3, name: "Silica", generation: 0, perfectGeneration: 0, buildings: 2, efficiency: null, wasted: 0, avgProductivity: 0 },
      { guid: 4, name: "Sheep", generation: 13.7, perfectGeneration: 28.1, buildings: 4, efficiency: 0.49, wasted: 14.4, avgProductivity: 220 },
    ],
  });

  const root = new Element();
  const cleanup = await renderEfficiency(root, island.id, { islands: [island], alerts: [], status: {} });

  const notes = nodes(root, ".cell-note");
  assert.equal(notes.length, 3, "zero output and missing potential explain themselves in the table");
  assert.ok(notes.some((node) => node.textContent === "not producing"));
  assert.ok(notes.some((node) => node.textContent === "no buildings"));
  assert.ok(notes.some((node) => node.textContent === "no potential"));
  const explainer = nodes(root, "details")[0].textContent;
  assert.match(explainer, /full storage/, "a full storage is named as a common reason for idle buildings");
  assert.match(explainer, /the data does not say which/,
    "the expanded explanation names candidates but does not pretend to know which one it is");
  assert.match(rowText(root), /220%/, "productivity above 100 % is shown as it is");
  cleanup();
});

test("a good without local production is marked quietly and is not a warning", async () => {
  i18n.lang = "en";
  api.products = async () => ({ island, products });
  const root = new Element();
  // Wheat is short on this island, but the island has no building for it:
  // the engine reports no local production (severity info), not a deficit.
  const store = {
    islands: [island],
    alerts: [
      { islandId: island.id, productGuid: 1, rule: "productivity_drop", severity: "warning", detail: "test" },
      { islandId: island.id, productGuid: 2, rule: "no_local_production", severity: "info", detail: "delta -4.0 for 3 samples" },
    ],
    status: {},
  };
  const cleanup = await renderIslandDetail(root, island.id, store);

  assert.equal(nodes(root, ".import-tag").length, 1, "exactly the imported good carries the tag");
  // The tag sits in the button that opens the possible sources (below),
  // which carries the explanation.
  const tags = nodes(root, ".sources-toggle");
  assert.equal(tags.length, 1);
  assert.match(tags[0].parent.textContent, /Wheat/);
  assert.match(tags[0].textContent, /import needed/);
  assert.match(tags[0].getAttribute("title"), /cannot see whether or how the good is actually delivered/,
    "the tag does not claim a delivery the data cannot show");
  assert.match(tags[0].getAttribute("title"), /Not counted as a warning/);
  assert.equal(nodes(root, ".alert-marker").length, 1, "only the real warning gets the warning marker");
  assert.match(root.textContent, /1 Warnings/, "the summary counts the warning, not the import");

  button(root, "Warnings").click();
  assert.match(rowText(root), /Bread/);
  assert.doesNotMatch(rowText(root), /Wheat/, "the warnings filter leaves it out");
  cleanup();
});

test("German is a full translation, not a fallback", async () => {
  i18n.lang = "de";
  api.products = async () => ({ island, products });
  const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, storeWith());

  assert.ok(button(root, "Defizite"), "the filters are translated");
  assert.match(nodes(root, "thead")[0].textContent, /Produktion\/min/);
  assert.match(rowText(root), /\+2,0/, "German writes 2,0 - the number format follows the language");
  cleanup();
  i18n.lang = "en";
});

// --- possible production sources ---------------------------------------------

const settle = () => new Promise((resolve) => setImmediate(resolve));

// An island that consumes Wheat (guid 2) without producing it, and the store
// saying so the way the rule engine does.
const importStore = () => ({
  islands: [island],
  alerts: [{ islandId: island.id, productGuid: 2, rule: "no_local_production", severity: "info", detail: "delta -4.0 for 3 samples" }],
  status: {},
});

const sourcesAnswer = (groups, ready = true) => ({
  island,
  product: { guid: 2, name: "Wheat" },
  ready,
  tick: ready ? 152525000 : null,
  groups,
});

const twoProvinces = sourcesAnswer([
  { sessionGuid: 3245, sessionName: "Latium", own: true, sources: [
    { id: "3245-3", islandId: 3, name: "Megaron", delta: 3.5 },
    { id: "3245-2", islandId: 2, name: "Agathea", delta: 2 },
  ] },
  { sessionGuid: 6627, sessionName: "Albion", own: false, sources: [
    { id: "6627-1", islandId: 1, name: "Argantum", delta: 4.25 },
    { id: "6627-2", islandId: 2, name: "Eboracum", delta: 0.03 },
  ] },
]);

// Words that would claim more than the game data shows: a delivery, a route,
// a supplier, or advice the data cannot back.
const overclaims = /supplier|supplies|delivers|is delivered|trade route to|must import|need to build|Lieferant|liefert|muss importieren|musst .* bauen/i;

test("possible production sources open under the row, grouped by province", async () => {
  i18n.lang = "en";
  api.products = async () => ({ island, products });
  const asked = [];
  api.sources = async (islandId, guid) => { asked.push(`${islandId}/${guid}`); return twoProvinces; };
  const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, importStore());

  const toggle = nodes(root, ".sources-toggle")[0];
  assert.equal(toggle.tagName, "button", "the tag is a real button: keyboard, screen reader and touch know it");
  assert.equal(toggle.getAttribute("aria-expanded"), "false");
  assert.match(toggle.getAttribute("title"), /cannot see whether or how the good is actually delivered/,
    "the tag keeps its explanation");
  assert.equal(nodes(root, ".sources-row").length, 0, "closed rows cost nothing: no block, no request");
  assert.deepEqual(asked, []);

  // As from the keyboard: the button has the focus, Enter clicks it.
  toggle.focus();
  toggle.click();
  assert.equal(nodes(root, ".sources-row").length, 1, "the row opens at once");
  assert.match(nodes(root, ".sources-row")[0].textContent, /Loading/);
  await settle();

  assert.deepEqual(asked, [`${island.id}/2`], "asked once, for this island and this good");
  const rows = nodes(root, "tbody")[0].children;
  const wheat = rows.findIndex((row) => row.textContent.includes("Wheat"));
  assert.equal(rows[wheat + 1].className, "sources-row", "the sources sit directly under their good");
  const detail = rows[wheat + 1].children[0];
  assert.equal(detail.colSpan, 5, "one cell across the whole table, which the phone layout leaves unpinned");

  const text = detail.textContent;
  assert.match(text, /Possible production sources/);
  assert.match(text, /Latium.*Megaron.*Local balance \+3\.5\/min.*Agathea.*\+2\.0\/min.*Albion.*Argantum.*\+4\.3\/min/,
    "the server's order, which is own province first and highest balance first");
  assert.match(text, /Eboracum.*Local balance \+0\.03\/min/, "a small positive balance is not written as +0.0");
  assert.match(text, /Possible sources only\. Trade routes and actual deliveries are not available in the game data\./);
  assert.doesNotMatch(text, overclaims);
  const link = nodes(detail, "a").find((a) => a.textContent === "Argantum");
  assert.equal(link.href, "#/island/6627-1", "a source is a way to its island");
  assert.equal(nodes(root, ".sources-toggle")[0].getAttribute("aria-expanded"), "true");
  assert.equal(document.activeElement, nodes(root, ".sources-toggle")[0],
    "the table is rebuilt on the click and again on the answer, and the focus has to survive both");

  // Another island's snapshot asks again. The same answer leaves the table
  // alone - a tick has ten snapshots, and none of them should rebuild it.
  const before = nodes(root, ".sources-row")[0];
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: "6627-1" } }));
  await settle();
  assert.equal(asked.length, 2, "asked again after the snapshot");
  assert.equal(nodes(root, ".sources-row")[0], before, "an unchanged answer does not rebuild the table");

  nodes(root, ".sources-toggle")[0].click();
  assert.equal(nodes(root, ".sources-row").length, 0, "a second click closes it");
  cleanup();
});

test("possible production sources say what is not known and what was not found", async () => {
  i18n.lang = "en";
  api.products = async () => ({ island, products });
  let answer = sourcesAnswer([], false);
  api.sources = async () => answer;
  const root = new Element();
  const store = importStore();
  const cleanup = await renderIslandDetail(root, island.id, store);
  nodes(root, ".sources-toggle")[0].click();
  await settle();
  assert.match(nodes(root, ".sources-row")[0].textContent, /Waiting for a complete statistics tick/,
    "right after connecting nothing is known yet, which is not the same as none");

  // A snapshot of any island may complete the tick: the open row asks again.
  answer = sourcesAnswer([]);
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: "6627-1" } }));
  await settle();
  const text = nodes(root, ".sources-row")[0].textContent;
  assert.match(text, /No island with a positive local balance found\./);
  assert.doesNotMatch(text, /no supplier|not being delivered|need to build/i,
    "none found is not a statement about deliveries or about what to build");

  // Only another province: shown as it is, nothing more.
  answer = sourcesAnswer([{ sessionGuid: 6627, sessionName: "Albion", own: false, sources: [
    { id: "6627-1", islandId: 1, name: "Argantum", delta: 4.3 },
  ] }]);
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: "6627-1" } }));
  await settle();
  assert.match(nodes(root, ".sources-row")[0].textContent, /Albion.*Argantum.*\+4\.3\/min/);

  // A failed request says so, and the next answer - the same as before the
  // failure - replaces the error again.
  api.sources = async () => { throw new Error("connection refused"); };
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: "6627-1" } }));
  await settle();
  assert.match(nodes(root, ".sources-row")[0].textContent, /Error: connection refused/);
  api.sources = async () => answer;
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: "6627-1" } }));
  await settle();
  assert.doesNotMatch(nodes(root, ".sources-row")[0].textContent, /connection refused/);
  assert.match(nodes(root, ".sources-row")[0].textContent, /Argantum/);

  // The good stops being "import needed": the control and the open row go.
  const alert = store.alerts.pop();
  document.dispatchEvent(new CustomEvent("tabularium-alerts-changed"));
  assert.equal(nodes(root, ".sources-toggle").length, 0, "only goods marked import needed get the control");
  assert.equal(nodes(root, ".sources-row").length, 0);
  // When it comes back, it comes back closed.
  store.alerts.push(alert);
  document.dispatchEvent(new CustomEvent("tabularium-alerts-changed"));
  assert.equal(nodes(root, ".sources-toggle")[0].getAttribute("aria-expanded"), "false");
  assert.equal(nodes(root, ".sources-row").length, 0);
  cleanup();
});

test("possible production sources in German", async () => {
  i18n.lang = "de";
  api.products = async () => ({ island, products });
  let answer = twoProvinces;
  api.sources = async () => answer;
  const root = new Element();
  const cleanup = await renderIslandDetail(root, island.id, importStore());
  nodes(root, ".sources-toggle")[0].click();
  await settle();
  const text = nodes(root, ".sources-row")[0].textContent;
  assert.match(text, /Mögliche Produktionsquellen/);
  assert.match(text, /Lokale Bilanz \+3,5\/min/, "German writes 3,5");
  assert.match(text, /Nur mögliche Quellen\. Handelsrouten und tatsächliche Lieferungen sind in den Spieldaten nicht verfügbar\./);
  assert.doesNotMatch(text, overclaims);
  assert.match(nodes(root, ".sources-toggle")[0].getAttribute("aria-label"), /Importbedarf: Mögliche Produktionsquellen anzeigen/);

  answer = sourcesAnswer([]);
  document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: { id: island.id } }));
  await settle();
  assert.match(nodes(root, ".sources-row")[0].textContent, /Keine Insel mit positivem lokalem Saldo gefunden\./);
  cleanup();
  i18n.lang = "en";
});

test("the warnings page opens the same sources for each import needed entry", async () => {
  i18n.lang = "en";
  const asked = [];
  api.sources = async (islandId, guid) => { asked.push(`${islandId}/${guid}`); return twoProvinces; };
  const store = importStore();
  store.alerts.push(
    { islandId: island.id, productGuid: 1, productName: "Bread", islandName: "Juliana", rule: "deficit", severity: "warning", detail: "x", raisedAt: new Date().toISOString() },
  );
  store.alerts[0].productName = "Wheat";
  store.alerts[0].islandName = "Juliana";
  store.alerts[0].raisedAt = new Date().toISOString();
  const root = new Element();
  const cleanup = await renderAlerts(root, store, false);

  const toggles = nodes(root, ".sources-toggle");
  assert.equal(toggles.length, 1, "the import needed entry has the control, the warning does not");
  assert.match(toggles[0].textContent, /Import needed/);
  toggles[0].click();
  await settle();
  assert.deepEqual(asked, [`${island.id}/2`]);
  const detail = nodes(root, ".sources-row")[0].children[0];
  assert.equal(detail.colSpan, 5);
  assert.match(detail.textContent, /Possible production sources.*Megaron.*Local balance \+3\.5\/min/);
  assert.match(detail.textContent, /Possible sources only/);
  cleanup();
});
