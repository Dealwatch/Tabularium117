// Shared island context and native navigation; no extra requests or dependencies.
import { i18n } from "../i18n.js";

export function islandNavigation(islandId, store, active) {
  const root = document.createElement("div");
  root.className = "island-context";
  const island = store.islands.find((i) => i.id === islandId);
  const eyebrow = document.createElement("p"); eyebrow.className = "eyebrow";
  eyebrow.textContent = island?.sessionName || i18n.t("islandsHeading");
  const row = document.createElement("div"); row.className = "heading-row";
  const title = document.createElement("h1"); title.textContent = island?.name || islandId;
  const select = document.createElement("select");
  select.setAttribute("aria-label", i18n.t("islandSwitch"));
  for (const i of store.islands) {
    const option = document.createElement("option"); option.value = i.id;
    option.textContent = `${i.name} · ${i.sessionName}`; option.selected = i.id === islandId;
    select.append(option);
  }
  select.addEventListener("change", () => {
    window.location.hash = `#/island/${encodeURIComponent(select.value)}${active === "efficiency" ? "/efficiency" : ""}`;
  });
  row.append(title, select);
  const nav = document.createElement("nav"); nav.className = "view-tabs";
  nav.setAttribute("aria-label", i18n.t("viewGoods"));
  for (const [key, label, suffix] of [["goods", "goods", ""], ["efficiency", "efficiencyHeading", "/efficiency"]]) {
    const a = document.createElement("a"); a.href = `#/island/${encodeURIComponent(islandId)}${suffix}`;
    a.textContent = i18n.t(label); if (key === active) a.setAttribute("aria-current", "page");
    nav.append(a);
  }
  root.append(eyebrow, row, nav);
  return root;
}
