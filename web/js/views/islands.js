// views/islands.js -- the product table for one island (KONZEPT.md section 2.1/2.2).

import { i18n } from "../i18n.js";
import { api } from "../api.js";
import { byProduct, ruleLabel } from "../alerts.js";
import { renderIslandHeader } from "./island-header.js";

const numberFormatCache = new Map();

function numberFormat(fractionDigits) {
  const locale = i18n.lang === "de" ? "de-DE" : "en-US";
  const key = `${locale}:${fractionDigits}`;
  if (!numberFormatCache.has(key)) {
    numberFormatCache.set(
      key,
      new Intl.NumberFormat(locale, { minimumFractionDigits: fractionDigits, maximumFractionDigits: fractionDigits }),
    );
  }
  return numberFormatCache.get(key);
}

const columns = [
  { key: "name", label: "colName", numeric: false },
  { key: "generation", label: "colGeneration", numeric: true },
  { key: "consumption", label: "colConsumption", numeric: true },
  { key: "delta", label: "colDelta", numeric: true },
  { key: "buildings", label: "colBuildings", numeric: true },
];

// renderIslandDetail fetches the island's products and mounts the table. It
// returns an unmount function that stops listening for live updates.
export async function renderIslandDetail(container, islandId, store) {
  let sortKey = "delta";
  let sortAsc = true; // deficits (most negative delta) first by default
  let search = "";

  // The island's name, the island switch and the tab to the efficiency view
  // all live in the shared header now, so this view only owns its table.
  const header = renderIslandHeader(container, islandId, store, "products");

  const wrapper = document.createElement("div");
  const summaryLine = document.createElement("p");
  summaryLine.className = "muted summary-line";

  const toolbar = document.createElement("div");
  toolbar.className = "toolbar";
  const search_input = document.createElement("input");
  search_input.type = "search";
  search_input.placeholder = i18n.t("searchPlaceholder");
  search_input.setAttribute("data-i18n-placeholder", "searchPlaceholder");
  toolbar.append(search_input);

  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const tbody = document.createElement("tbody");
  table.append(thead, tbody);

  // A phone screen is narrower than this table; it scrolls inside its own
  // box instead of dragging the whole page sideways (task T7.4).
  const tableBox = document.createElement("div");
  tableBox.className = "table-scroll";
  tableBox.append(table);
  wrapper.append(summaryLine, toolbar, tableBox);
  container.append(wrapper);

  let productsDTO = null;

  function headerRow() {
    const tr = document.createElement("tr");
    for (const col of columns) {
      const th = document.createElement("th");
      if (col.numeric) th.className = "num";
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = i18n.t(col.label) + (sortKey === col.key ? (sortAsc ? " ↑" : " ↓") : "");
      btn.addEventListener("click", () => {
        if (sortKey === col.key) sortAsc = !sortAsc;
        else { sortKey = col.key; sortAsc = true; }
        renderTable();
      });
      th.append(btn);
      tr.append(th);
    }
    thead.replaceChildren(tr);
  }

  function deltaClass(delta) {
    if (delta < 0) return "delta-negative";
    if (delta > 0) return "delta-positive";
    return "";
  }

  function renderTable() {
    if (!productsDTO) return;
    headerRow();
    const island = productsDTO.island;
    const alertCount = store.alerts.filter((a) => a.islandId === islandId).length;
    // The name and the session are in the header; repeating them here only
    // made the line long enough to be skipped. What is left are the counts.
    header.update(island);
    summaryLine.textContent = `${island.products} ${i18n.t("products")} · `
      + `${island.deficits} ${i18n.t("deficits")}`
      + (alertCount > 0 ? ` · ${alertCount} ${i18n.t("alertsHeading")}` : "");

    // Warnings are per (island, product); a row carries a marker when the
    // rule engine has one open for it.
    const alerted = byProduct(store.alerts, islandId);

    let rows = productsDTO.products;
    if (search.trim()) {
      const needle = search.trim().toLowerCase();
      rows = rows.filter((p) => p.name.toLowerCase().includes(needle));
    }
    rows = [...rows].sort((a, b) => {
      let cmp;
      if (sortKey === "name") cmp = a.name.localeCompare(b.name);
      else cmp = a[sortKey] - b[sortKey];
      return sortAsc ? cmp : -cmp;
    });

    tbody.replaceChildren();
    if (rows.length === 0) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = columns.length;
      if (productsDTO.products.length === 0) {
        // An empty island right after a savegame is loaded is not an island
        // without production - it is one whose first statistics tick has not
        // arrived yet (docs/protocol.md). Saying "no production" there would
        // be wrong for up to two minutes.
        td.textContent = store.status?.warmingUp ? i18n.t("warmingUp") : i18n.t("noProduction");
        if (store.status?.warmingUp) tr.className = "no-data";
      } else {
        td.textContent = "";
      }
      tr.append(td);
      tbody.append(tr);
      return;
    }
    const fmt1 = numberFormat(1);
    const fmt0 = numberFormat(0);
    for (const p of rows) {
      const tr = document.createElement("tr");
      tr.className = "clickable";
      tr.tabIndex = 0;
      tr.addEventListener("click", () => {
        window.location.hash = `#/island/${encodeURIComponent(islandId)}/product/${encodeURIComponent(p.guid)}`;
      });
      tr.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") tr.click();
      });

      const nameTd = document.createElement("td");
      const warnings = alerted.get(String(p.guid));
      if (warnings) {
        const marker = document.createElement("span");
        marker.className = "alert-marker";
        marker.textContent = "\u26A0";
        marker.title = `${i18n.t("alertMarker")}: ${warnings.map((a) => `${ruleLabel(a.rule)} (${a.detail})`).join(", ")}`;
        marker.setAttribute("aria-label", i18n.t("alertMarker"));
        nameTd.append(marker, document.createTextNode(` ${p.name}`));
      } else {
        nameTd.textContent = p.name;
      }
      const genTd = document.createElement("td");
      genTd.className = "num";
      genTd.textContent = fmt1.format(p.generation);
      const consTd = document.createElement("td");
      consTd.className = "num";
      consTd.textContent = fmt1.format(p.consumption);
      const deltaTd = document.createElement("td");
      deltaTd.className = `num ${deltaClass(p.delta)}`;
      deltaTd.textContent = fmt1.format(p.delta);
      const buildingsTd = document.createElement("td");
      buildingsTd.className = "num";
      buildingsTd.textContent = fmt0.format(p.buildings);

      tr.append(nameTd, genTd, consTd, deltaTd, buildingsTd);
      tbody.append(tr);
    }
  }

  search_input.addEventListener("input", () => {
    search = search_input.value;
    renderTable();
  });

  async function load() {
    productsDTO = await api.products(islandId);
    renderTable();
  }

  await load();

  const onSnapshot = (e) => {
    if (e.detail && e.detail.id === islandId) {
      load().catch((err) => console.error("tabularium117: cannot refresh products", err));
    }
  };
  document.addEventListener("tabularium-snapshot", onSnapshot);

  const onLang = () => renderTable();
  document.addEventListener("tabularium-lang-changed", onLang);

  const onAlerts = () => renderTable();
  document.addEventListener("tabularium-alerts-changed", onAlerts);

  return () => {
    document.removeEventListener("tabularium-snapshot", onSnapshot);
    document.removeEventListener("tabularium-lang-changed", onLang);
    document.removeEventListener("tabularium-alerts-changed", onAlerts);
  };
}
