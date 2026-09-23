// views/efficiency.js -- the efficiency view (KONZEPT.md section 2.3).

import { i18n } from "../i18n.js";
import { api } from "../api.js";
import { renderIslandHeader } from "./island-header.js";

function locale() {
  return i18n.lang === "de" ? "de-DE" : "en-US";
}

// The reason for zero output must be visible on a phone and to a screen
// reader, not hidden in a hover-only title attribute.
function valueWithNote(value, note) {
  const wrapper = document.createElement("span");
  wrapper.append(document.createTextNode(value));
  const label = document.createElement("span");
  label.className = "cell-note";
  label.textContent = note;
  wrapper.append(label);
  return wrapper;
}

export async function renderEfficiency(container, islandId, store) {
  // Same reason as in views/islands.js: an answer that was overtaken must
  // not paint over the newer one.
  let disposed = false;
  let request = 0;

  // The heading, the island switch and the way back to the goods table are
  // the shared header's job (views/island-header.js).
  const header = renderIslandHeader(container, islandId, store, "efficiency");

  const intro = document.createElement("p");
  intro.className = "muted efficiency-intro";
  intro.textContent = i18n.t("efficiencyIntro");
  const details = document.createElement("details");
  details.className = "efficiency-explainer";
  const summary = document.createElement("summary");
  summary.textContent = i18n.t("efficiencyDetails");
  const meanings = document.createElement("dl");
  for (const [term, meaning] of [
    ["colEfficiency", "efficiencyMeaning"],
    ["colProductivity", "productivityMeaning"],
    ["colWasted", "unusedMeaning"],
    ["productivityIdleNote", "productivityIdleMeaning"],
    ["productivityNoBuildingsNote", "productivityNoBuildingsMeaning"],
    ["productivityNoPotentialNote", "productivityNoPotentialMeaning"],
  ]) {
    const dt = document.createElement("dt");
    dt.textContent = i18n.t(term);
    const dd = document.createElement("dd");
    dd.textContent = i18n.t(meaning);
    meanings.append(dt, dd);
  }
  details.append(summary, meanings);

  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const tbody = document.createElement("tbody");
  table.append(thead, tbody);

  // A phone screen is narrower than this table; it scrolls inside its own
  // box instead of dragging the whole page sideways.
  const tableBox = document.createElement("div");
  tableBox.className = "table-scroll";
  tableBox.append(table);
  container.append(intro, details, tableBox);

  function render(dto) {
    header.update(dto.island);
    const headRow = document.createElement("tr");
    for (const [label, numeric] of [
      [i18n.t("colName"), false],
      [i18n.t("colGeneration"), true],
      [i18n.t("colPerfect"), true],
      [i18n.t("colEfficiency"), true],
      [i18n.t("colProductivity"), true],
      [i18n.t("colWasted"), true],
    ]) {
      const th = document.createElement("th");
      if (numeric) th.className = "num";
      th.textContent = label;
      headRow.append(th);
    }
    thead.replaceChildren(headRow);

    tbody.replaceChildren();
    if (dto.products.length === 0) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 6;
      td.textContent = i18n.t("noEfficiencyData");
      tr.append(td);
      tbody.append(tr);
      return;
    }
    const fmt1 = new Intl.NumberFormat(locale(), { minimumFractionDigits: 1, maximumFractionDigits: 1 });
    const pct = new Intl.NumberFormat(locale(), { style: "percent", maximumFractionDigits: 0 });
    // avgProductivity arrives already in percent (SummedProductivity /
    // AmountOfBuildings x 100, KONZEPT.md section 12), so it goes back to a
    // ratio before it is formatted. Writing the sign by hand instead put a
    // space in front of it in English, where the two percentages in this
    // table then disagreed with each other ("49%" beside "220 %").
    const percent = (valueInPercent) => pct.format(valueInPercent / 100);

    // The server already sorts by wasted desc; products with efficiency ==
    // null (perfectGeneration 0) have wasted 0 or negative and are moved to
    // the very bottom regardless, since there is nothing to compare here.
    const sorted = [...dto.products].sort((a, b) => {
      const aNull = a.efficiency === null;
      const bNull = b.efficiency === null;
      if (aNull !== bNull) return aNull ? 1 : -1;
      return b.wasted - a.wasted;
    });

    for (const p of sorted) {
      const tr = document.createElement("tr");
      if (p.efficiency === null) tr.className = "no-data";

      const nameTd = document.createElement("td");
      nameTd.textContent = p.name;
      const genTd = document.createElement("td");
      genTd.className = "num";
      genTd.textContent = fmt1.format(p.generation);
      const perfectTd = document.createElement("td");
      perfectTd.className = "num";
      perfectTd.textContent = fmt1.format(p.perfectGeneration);

      const effTd = document.createElement("td");
      effTd.className = "num";
      if (p.efficiency === null) {
        effTd.textContent = "-";
      } else {
        // Bar and number share one line, bar first: the bars line up into a
        // column that can be read top to bottom, which is the whole point of
        // drawing them at all.
        const cell = document.createElement("div");
        cell.className = "efficiency-cell";
        const bar = document.createElement("div");
        bar.className = "efficiency-bar";
        const fill = document.createElement("span");
        const ratio = Math.max(0, Math.min(1, p.efficiency));
        fill.style.width = `${(ratio * 100).toFixed(0)}%`;
        bar.append(fill);
        const value = document.createElement("span");
        value.textContent = pct.format(p.efficiency);
        cell.append(bar, value);
        effTd.append(cell);
      }

      // Productivity is a different question from efficiency: how hard the
      // buildings run, rather than how the output compares with its optimum
      // (KONZEPT.md section 2.3). Without buildings there is nothing to
      // average, and the field is then a zero that means "not applicable".
      //
      // The row itself explains the two ways of producing nothing. Longer
      // explanations are available in the details above the table.
      const productivityTd = document.createElement("td");
      productivityTd.className = "num";
      if (p.perfectGeneration === 0 && p.buildings === 0) {
        productivityTd.append(valueWithNote("–", i18n.t("productivityNoBuildingsNote")));
      } else if (p.perfectGeneration === 0) {
        productivityTd.append(valueWithNote(percent(p.avgProductivity), i18n.t("productivityNoPotentialNote")));
      } else if (p.generation === 0 && p.perfectGeneration > 0) {
        productivityTd.append(valueWithNote(percent(p.avgProductivity), i18n.t("productivityIdleNote")));
      } else {
        productivityTd.textContent = percent(p.avgProductivity);
      }

      const wastedTd = document.createElement("td");
      wastedTd.className = "num";
      wastedTd.textContent = fmt1.format(p.wasted);

      tr.append(nameTd, genTd, perfectTd, effTd, productivityTd, wastedTd);
      tbody.append(tr);
    }
  }

  async function load() {
    const current = ++request;
    const dto = await api.efficiency(islandId);
    if (disposed || current !== request) return;
    render(dto);
  }

  await load();

  const onSnapshot = (e) => {
    if (e.detail && e.detail.id === islandId) {
      load().catch((err) => console.error("tabularium117: cannot refresh efficiency", err));
    }
  };
  document.addEventListener("tabularium-snapshot", onSnapshot);
  const onLang = () => load();
  document.addEventListener("tabularium-lang-changed", onLang);

  return () => {
    disposed = true;
    document.removeEventListener("tabularium-snapshot", onSnapshot);
    document.removeEventListener("tabularium-lang-changed", onLang);
  };
}
