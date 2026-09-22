// views/efficiency.js -- the efficiency view (KONZEPT.md section 2.3).

import { i18n } from "../i18n.js";
import { api } from "../api.js";
import { renderIslandHeader } from "./island-header.js";

function locale() {
  return i18n.lang === "de" ? "de-DE" : "en-US";
}

// hint renders a value that needs a sentence to be read correctly. The
// explanation is a native title tooltip - no build step, no popup library -
// and the dotted underline is what tells a reader there is one at all. A
// phone has no hover, so the same sentence is in the paragraph above the
// table as well (i18n efficiencyExplain).
function hint(text, explanation) {
  const span = document.createElement("span");
  span.className = "hint";
  span.title = explanation;
  span.textContent = text;
  return span;
}

export async function renderEfficiency(container, islandId, store) {
  // The heading, the island switch and the way back to the goods table are
  // the shared header's job (views/island-header.js).
  const header = renderIslandHeader(container, islandId, store, "efficiency");

  const explain = document.createElement("p");
  explain.className = "muted";
  explain.textContent = i18n.t("efficiencyExplain");

  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const tbody = document.createElement("tbody");
  table.append(thead, tbody);

  // A phone screen is narrower than this table; it scrolls inside its own
  // box instead of dragging the whole page sideways (task T7.4).
  const tableBox = document.createElement("div");
  tableBox.className = "table-scroll";
  tableBox.append(table);
  container.append(explain, tableBox);

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
      // The numbers alone cannot separate the two ways of producing nothing:
      // "0.0 / 10.0 / 0 %" means the buildings stand still, while "-" means
      // there are no buildings at all. Both get the sentence that says so,
      // because a reader new to the view reads them as the same row.
      const productivityTd = document.createElement("td");
      productivityTd.className = "num";
      if (p.perfectGeneration === 0 && p.avgProductivity === 0) {
        productivityTd.append(hint("-", i18n.t("productivityNoBuildingsHint")));
      } else if (p.generation === 0 && p.perfectGeneration > 0) {
        productivityTd.append(hint(percent(p.avgProductivity), i18n.t("productivityIdleHint")));
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
    const dto = await api.efficiency(islandId);
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
    document.removeEventListener("tabularium-snapshot", onSnapshot);
    document.removeEventListener("tabularium-lang-changed", onLang);
  };
}
