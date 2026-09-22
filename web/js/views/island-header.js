// views/island-header.js -- the header the island's views share: which island
// is on screen, how to get to another one, and the tabs between the island's
// two tables.
//
// It exists because those three things used to be spread over the views: the
// island's name was repeated inside a summary line, switching islands meant
// going back to the sidebar, and the efficiency view was reached through a
// link in a toolbar and left through a "back" link. One header, one place.

import { i18n } from "../i18n.js";

// TABS are the island's own views. The history of a single product is not one
// of them: it is opened from a row and returns to that row, so it keeps its
// back link instead.
const TABS = [
  { id: "products", key: "tabProducts", path: (id) => `#/island/${encodeURIComponent(id)}` },
  { id: "efficiency", key: "efficiencyHeading", path: (id) => `#/island/${encodeURIComponent(id)}/efficiency` },
];

// renderIslandHeader mounts the header and returns an update function. The
// caller calls it again when fresh island data arrives, because a player can
// rename an island while the view is open (docs/protocol.md).
export function renderIslandHeader(container, islandId, store, activeTab) {
  const header = document.createElement("header");
  header.className = "island-header";

  const titleBox = document.createElement("div");
  titleBox.className = "island-title";
  const session = document.createElement("p");
  session.className = "eyebrow";
  const title = document.createElement("h2");
  titleBox.append(session, title);

  // Switching island keeps the current tab: someone comparing the efficiency
  // of two islands should not be dropped back onto the goods table.
  const select = document.createElement("select");
  select.className = "island-switch";
  select.setAttribute("aria-label", i18n.t("islandSwitch"));
  select.addEventListener("change", () => {
    const tab = TABS.find((t) => t.id === activeTab) || TABS[0];
    window.location.hash = tab.path(select.value);
  });

  const row = document.createElement("div");
  row.className = "island-header-row";
  row.append(titleBox, select);

  const tabs = document.createElement("nav");
  tabs.className = "tabs";
  tabs.setAttribute("aria-label", i18n.t("islandsHeading"));
  for (const tab of TABS) {
    const link = document.createElement("a");
    link.href = tab.path(islandId);
    link.textContent = i18n.t(tab.key);
    if (tab.id === activeTab) link.setAttribute("aria-current", "page");
    tabs.append(link);
  }

  header.append(row, tabs);
  container.append(header);

  function update(island) {
    const current = island || store.islands.find((i) => i.id === islandId);
    title.textContent = current?.name || islandId;
    session.textContent = current?.sessionName || "";

    select.replaceChildren();
    for (const other of store.islands) {
      const option = document.createElement("option");
      option.value = other.id;
      option.textContent = `${other.name} · ${other.sessionName}`;
      option.selected = other.id === islandId;
      select.append(option);
    }
    // With one island there is nothing to switch to, and an empty-looking
    // dropdown would only invite a click that does nothing.
    select.hidden = store.islands.length < 2;
  }

  update(null);
  return { update };
}
