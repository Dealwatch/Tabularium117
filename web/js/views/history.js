// views/history.js -- the uPlot chart for one product (KONZEPT.md section 2.2).

import { islandNavigation } from "./navigation.js";
import { i18n } from "../i18n.js";
import { api, ApiError } from "../api.js";
import uPlot from "../../vendor/uplot/uPlot.esm.js";

// The long ranges exist because the game ticks about every two minutes: a
// day is roughly 720 points, a week of those is mostly ten-minute means
// (KONZEPT.md section 2.2).
const RANGES = ["1h", "4h", "24h", "7d", "session"];
const THROTTLE_MS = 5000;

function seriesColors() {
  const dark = document.documentElement.getAttribute("data-theme") === "dark"
    || (!document.documentElement.hasAttribute("data-theme")
      && window.matchMedia?.("(prefers-color-scheme: dark)").matches);
  return dark
    ? { generation: "#4fce7a", consumption: "#ff6b5c", delta: "#6fa2ff", grid: "#333338", text: "#ececec" }
    : { generation: "#1e8449", consumption: "#c0392b", delta: "#2f6fed", grid: "#d9d9dd", text: "#1c1c1e" };
}

export async function renderHistory(container, islandId, guid, store) {
  let range = "1h";
  let chart = null;
  let data = null;
  let lastLoadAt = 0;
  let disposed = false;
  let request = 0;

  const back = document.createElement("a");
  back.href = `#/island/${encodeURIComponent(islandId)}`;
  back.className = "back-link";
  back.textContent = `← ${i18n.t("helpBack")}`;

  const heading = document.createElement("h2");
  const rangeBar = document.createElement("div");
  rangeBar.className = "range-buttons";
  const rangeButtons = {};
  for (const r of RANGES) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = i18n.t(`range${r === "session" ? "Session" : r.charAt(0).toUpperCase() + r.slice(1)}`);
    btn.addEventListener("click", () => {
      range = r;
      updateRangeButtons();
      load();
    });
    rangeButtons[r] = btn;
    rangeBar.append(btn);
  }
  function updateRangeButtons() {
    for (const r of RANGES) rangeButtons[r].setAttribute("aria-pressed", String(r === range));
  }
  updateRangeButtons();

  const chartContainer = document.createElement("div");
  chartContainer.id = "chart-container";
  const legend = document.createElement("div");
  legend.className = "legend";
  const message = document.createElement("p");

  const panel = document.createElement("section"); panel.className = "data-panel chart-panel";
  const units = document.createElement("p"); units.className = "muted"; units.textContent = i18n.t("perMinute");
  container.append(islandNavigation(islandId, store, "history"), back, heading, panel);
  panel.append(rangeBar, units, chartContainer, legend, message);

  function labelFor(key) {
    return i18n.t(`legend${key.charAt(0).toUpperCase() + key.slice(1)}`);
  }

  function renderLegend(latest) {
    legend.replaceChildren();
    const colors = seriesColors();
    for (const key of ["generation", "consumption", "delta"]) {
      const span = document.createElement("span");
      const swatch = document.createElement("span");
      swatch.className = "swatch";
      swatch.style.background = colors[key];
      const value = latest ? new Intl.NumberFormat(i18n.lang === "de" ? "de-DE" : "en-US", {minimumFractionDigits: 1, maximumFractionDigits: 1}).format(latest[key]) : "–";
      span.append(swatch, document.createTextNode(`${labelFor(key)}: ${value}`));
      legend.append(span);
    }
  }

  function buildChart(points, from, to) {
    chartContainer.replaceChildren();
    if (chart) { chart.destroy(); chart = null; }
    if (points.length === 0) {
      message.textContent = i18n.t("historyEmpty");
      renderLegend(null);
      return;
    }
    message.textContent = points.length === 1 ? i18n.t("sparseHistory") : "";
    const colors = seriesColors();
    // The series mixes raw readings with compacted means - and, in an old
    // database, means of two different widths. They are one series all the
    // same: every point is a measurement at its own time, already ordered by
    // the server, so one line per value is the honest picture. `aggregated`
    // and `bucketMs` say how smoothed a point is, for anything that wants to
    // show it.
    const xs = points.map((p) => new Date(p.ts).getTime() / 1000);
    const series = {
      generation: points.map((p) => p.generation),
      consumption: points.map((p) => p.consumption),
      delta: points.map((p) => p.delta),
    };

    const opts = {
      width: chartContainer.clientWidth || 600,
      height: 260,
      series: [
        {},
        { label: labelFor("generation"), stroke: colors.generation, width: 2 },
        { label: labelFor("consumption"), stroke: colors.consumption, width: 2 },
        { label: labelFor("delta"), stroke: colors.delta, width: 2 },
      ],
      axes: [
        { stroke: colors.text, grid: { stroke: colors.grid } },
        { stroke: colors.text, grid: { stroke: colors.grid } },
      ],
      legend: { show: false },
      cursor: { show: true },
    };
    // Show the requested window, not uPlot's auto range: with one or two
    // points the auto range spans years and the chart becomes unreadable.
    if (from && to) {
      const fromSec = new Date(from).getTime() / 1000;
      const toSec = new Date(to).getTime() / 1000;
      if (Number.isFinite(fromSec) && Number.isFinite(toSec) && toSec > fromSec) {
        opts.scales = { x: { range: [fromSec, toSec] } };
      }
    }
    chart = new uPlot(opts, [xs, series.generation, series.consumption, series.delta], chartContainer);
    renderLegend({
      generation: series.generation[series.generation.length - 1],
      consumption: series.consumption[series.consumption.length - 1],
      delta: series.delta[series.delta.length - 1],
    });
  }

  const resizeObserver = new ResizeObserver(() => {
    if (chart) chart.setSize({ width: chartContainer.clientWidth || 600, height: 260 });
  });
  resizeObserver.observe(chartContainer);

  async function load() {
    lastLoadAt = Date.now();
    const current = ++request;
    try {
      const result = await api.history(islandId, guid, range);
      if (disposed || current !== request) return;
      data = result;
      heading.textContent = `${i18n.t("historyHeading")}: ${data.product.name}`;
      buildChart(data.points, data.from, data.to);
    } catch (err) {
      if (disposed || current !== request) return;
      if (err instanceof ApiError && err.status === 503) {
        data = null;
        if (chart) { chart.destroy(); chart = null; }
        chartContainer.replaceChildren();
        legend.replaceChildren();
        message.textContent = i18n.t("historyDisabled");
        return;
      }
      message.textContent = `${i18n.t("historyError")} (${err.message})`;
    }
  }

  await load();

  const onSnapshot = (e) => {
    if (e.detail && e.detail.id === islandId && Date.now() - lastLoadAt > THROTTLE_MS) {
      load().catch((err) => console.error("tabularium117: cannot refresh history", err));
    }
  };
  document.addEventListener("tabularium-snapshot", onSnapshot);

  const onTheme = () => { if (data && !disposed) buildChart(data.points, data.from, data.to); };
  document.addEventListener("tabularium-theme-changed", onTheme);
  const onLang = () => load();
  document.addEventListener("tabularium-lang-changed", onLang);

  return () => {
    disposed = true;
    document.removeEventListener("tabularium-theme-changed", onTheme);
    document.removeEventListener("tabularium-snapshot", onSnapshot);
    document.removeEventListener("tabularium-lang-changed", onLang);
    resizeObserver.disconnect();
    if (chart) chart.destroy();
  };
}
