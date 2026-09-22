// pipe-help.js -- the "how to enable the pipe" explanation shared by the
// home empty state and the #/help page (KONZEPT.md section 8).

import { i18n } from "./i18n.js";

export function buildPipeHelp() {
  const frag = document.createDocumentFragment();
  const intro = document.createElement("p");
  intro.textContent = i18n.t("helpPipeIntro");
  const ol = document.createElement("ol");
  for (const key of ["helpPipeStep1", "helpPipeStep2", "helpPipeStep3"]) {
    const li = document.createElement("li");
    li.textContent = i18n.t(key);
    ol.append(li);
  }
  const local = document.createElement("p");
  local.textContent = i18n.t("helpLocal");
  frag.append(intro, ol, local);
  return frag;
}
