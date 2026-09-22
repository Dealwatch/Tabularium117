// views/efficiency.js -- the efficiency view (KONZEPT.md section 2.3).

import { islandNavigation } from "./navigation.js";
import { i18n } from "../i18n.js";
import { api } from "../api.js";

function locale() {
  return i18n.lang === "de" ? "de-DE" : "en-US";
}

export async function renderEfficiency(container, islandId, store) {
  let disposed = false;
  let request = 0;
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
  tableBox.tabIndex = 0;
  tableBox.setAttribute("role", "region");
  tableBox.setAttribute("aria-label", i18n.t("efficiencyHeading"));
  tableBox.append(table);
  container.append(islandNavigation(islandId, store, "efficiency"), explain, tableBox);
  tableBox.classList.add("data-panel");

  function render(dto) {
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
    // AmountOfBuildings x 100, KONZEPT.md section 12), so it is formatted as
    // a plain number with a percent sign, not through a percent formatter
    // that would multiply it again.
    const fmt0 = new Intl.NumberFormat(locale(), { maximumFractionDigits: 0 });

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
        const bar = document.createElement("div");
        bar.className = "efficiency-bar";
        const fill = document.createElement("span");
        const ratio = Math.max(0, Math.min(1, p.efficiency));
        fill.style.width = `${(ratio * 100).toFixed(0)}%`;
        bar.append(fill);
        effTd.append(document.createTextNode(pct.format(p.efficiency) + " "), bar);
      }

      // Productivity is a different question from efficiency: how hard the
      // buildings run, rather than how the output compares with its optimum
      // (KONZEPT.md section 2.3). Without buildings there is nothing to
      // average, and the field is then a zero that means "not applicable".
      const productivityTd = document.createElement("td");
      productivityTd.className = "num";
      productivityTd.textContent = p.perfectGeneration === 0 && p.avgProductivity === 0
        ? "-"
        : `${fmt0.format(p.avgProductivity)} %`;

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
    if (!disposed && current === request) render(dto);
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
