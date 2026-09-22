import { groupIslands } from "../regions.js";
import { i18n } from "../i18n.js";

export function renderRegionOverview(store) {
  const root = document.createElement("div"); root.className = "region-overview";
  for (const group of groupIslands(store.islands)) {
    const section = document.createElement("section"); section.className = "overview-region";
    const heading = document.createElement("h2"); heading.textContent = group.name;
    const list = document.createElement("ul"); list.className = "overview-islands";
    for (const island of group.islands) {
      const item = document.createElement("li"); item.className = "overview-island";
      const link = document.createElement("a"); link.className = "overview-island-name";
      const url = `#/island/${encodeURIComponent(island.id)}`;
      link.href = url; link.textContent = island.name || `#${island.islandId}`;
      const count = document.createElement("span"); count.className = "muted";
      count.textContent = store.status?.warmingUp ? i18n.t("warmingUpShort") : `${island.products} ${i18n.t("products")}`;
      item.append(link, count);
      if (!store.status?.warmingUp) {
        const deficits = document.createElement("a"); deficits.href = url + "?filter=deficits";
        deficits.className = island.deficits > 0 ? "overview-deficit delta-negative" : "overview-deficit muted";
        deficits.textContent = i18n.t("deficitCount", { count: island.deficits });
        deficits.setAttribute("aria-label", `${link.textContent}: ${deficits.textContent}`);
        item.append(deficits);
      }
      list.append(item);
    }
    section.append(heading, list); root.append(section);
  }
  return root;
}
