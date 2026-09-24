// views/alerts.js -- the #/alerts page: the warnings that are open now, and
// optionally the recorded history (#/alerts?all=1).

import { i18n } from "../i18n.js";
import { api, ApiError } from "../api.js";
import { isWarning, ruleLabel } from "../alerts.js";
import { formatAge, formatTime, formatDateTime } from "../format.js";

// renderAlerts mounts the list. The active list comes from the shared store,
// which the event stream keeps fresh; the history is fetched once, because
// nothing pushes closed warnings.
export async function renderAlerts(container, store, showHistory) {
  const heading = document.createElement("h2");
  const toolbar = document.createElement("div");
  toolbar.className = "toolbar";
  const toggle = document.createElement("a");
  toggle.className = "back-link";
  const explain = document.createElement("p");
  explain.className = "muted";
  const message = document.createElement("p");
  message.className = "muted";
  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const tbody = document.createElement("tbody");
  table.append(thead, tbody);

  toolbar.append(toggle);
  // A phone screen is narrower than this table; it scrolls inside its own
  // box instead of dragging the whole page sideways.
  const tableBox = document.createElement("div");
  tableBox.className = "table-scroll";
  tableBox.append(table);

  // Goods an island consumes but does not produce are info, not warnings.
  // The active view lists them in a section of their own under the
  // warnings; the history, a list of warnings, leaves them out.
  const importsHeading = document.createElement("h3");
  const importsExplain = document.createElement("p");
  importsExplain.className = "muted";
  const importsTable = document.createElement("table");
  const importsThead = document.createElement("thead");
  const importsTbody = document.createElement("tbody");
  importsTable.append(importsThead, importsTbody);
  const importsBox = document.createElement("div");
  importsBox.className = "table-scroll";
  importsBox.append(importsTable);
  const importsSection = document.createElement("section");
  importsSection.className = "imports-section";
  importsSection.append(importsHeading, importsExplain, importsBox);

  container.append(heading, toolbar, explain, message, tableBox, importsSection);

  let history = null;
  let historyError = "";

  if (showHistory) {
    try {
      // Warnings only: the info alerts would fill the 200 rows up - an
      // island records one for every good it does not produce itself.
      history = await api.alerts(false, 200, false);
    } catch (err) {
      history = [];
      historyError = err instanceof ApiError ? err.message : String(err);
    }
  }

  function columns() {
    return showHistory
      ? ["colIsland", "colProduct", "colRule", "colDetail", "colSince", "colUntil"]
      : ["colIsland", "colProduct", "colRule", "colDetail", "colSince"];
  }

  function headerRow(target) {
    const tr = document.createElement("tr");
    for (const key of columns()) {
      const th = document.createElement("th");
      th.textContent = i18n.t(key);
      tr.append(th);
    }
    target.replaceChildren(tr);
  }

  function row(alert) {
    const tr = document.createElement("tr");

    const islandTd = document.createElement("td");
    const link = document.createElement("a");
    link.href = `#/island/${encodeURIComponent(alert.islandId)}`;
    link.textContent = alert.islandName || alert.islandId;
    islandTd.append(link);

    const productTd = document.createElement("td");
    const productLink = document.createElement("a");
    productLink.href = `#/island/${encodeURIComponent(alert.islandId)}/product/${encodeURIComponent(alert.productGuid)}`;
    productLink.textContent = alert.productName;
    productTd.append(productLink);

    const ruleTd = document.createElement("td");
    ruleTd.textContent = ruleLabel(alert.rule);

    const detailTd = document.createElement("td");
    detailTd.textContent = alert.detail;

    const sinceTd = document.createElement("td");
    sinceTd.textContent = i18n.t("alertSince", { age: formatAge(Date.now() - new Date(alert.raisedAt).getTime()) });
    sinceTd.title = formatDateTime(alert.raisedAt);

    tr.append(islandTd, productTd, ruleTd, detailTd, sinceTd);

    if (showHistory) {
      const untilTd = document.createElement("td");
      if (alert.clearedAt) {
        untilTd.textContent = formatTime(alert.clearedAt);
        untilTd.title = formatDateTime(alert.clearedAt);
      } else {
        untilTd.textContent = i18n.t("alertActive");
        untilTd.className = "delta-negative";
      }
      tr.append(untilTd);
    }
    return tr;
  }

  function render() {
    heading.textContent = showHistory ? i18n.t("alertsHistoryHeading") : i18n.t("alertsHeading");
    toggle.href = showHistory ? "#/alerts" : "#/alerts?all=1";
    toggle.textContent = showHistory ? i18n.t("alertsShowActive") : i18n.t("alertsShowHistory");
    explain.textContent = i18n.t("alertsExplain");

    const rows = showHistory ? history : store.alerts.filter(isWarning);
    headerRow(thead);
    tbody.replaceChildren();
    for (const alert of rows) tbody.append(row(alert));

    const imports = showHistory ? [] : store.alerts.filter((a) => !isWarning(a));
    importsSection.hidden = imports.length === 0;
    importsHeading.textContent = `${i18n.t("importsHeading")} (${imports.length})`;
    importsExplain.textContent = i18n.t("importsExplain");
    headerRow(importsThead);
    importsTbody.replaceChildren();
    for (const alert of imports) importsTbody.append(row(alert));

    if (historyError) {
      message.textContent = `${i18n.t("errorPrefix")} ${historyError}`;
      message.className = "error-line";
    } else if (rows.length === 0) {
      message.textContent = showHistory ? i18n.t("alertsHistoryEmpty") : i18n.t("alertsNone");
      message.className = "muted";
    } else {
      message.textContent = "";
    }
    table.hidden = rows.length === 0;
  }

  render();

  const onAlerts = () => {
    if (!showHistory) render();
  };
  const onLang = () => render();
  document.addEventListener("tabularium-alerts-changed", onAlerts);
  document.addEventListener("tabularium-lang-changed", onLang);

  return () => {
    document.removeEventListener("tabularium-alerts-changed", onAlerts);
    document.removeEventListener("tabularium-lang-changed", onLang);
  };
}
