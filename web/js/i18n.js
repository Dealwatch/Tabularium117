// i18n.js -- tiny DE/EN dictionary and language helpers.
//
// The API's ?lang= parameter uses the catalog's codes ("german"/"english",
// KONZEPT.md section 5), not the ISO codes the UI keeps in localStorage. This
// module owns the mapping between the two.

const STORAGE_KEY = "tabularium.lang";

const dictionaries = {
  en: {
    appName: "Tabularium 117",
    statusConnected: "connected",
    statusWaiting: "waiting for Anno 117",
    statusDisconnected: "disconnected",
    statusReplaying: "replaying",
    statusEnded: "recording ended",
    connectionDetails: "Details",
    sourceLabel: "Source",
    sourcePipe: "Anno 117",
    sourceReplay: "Recording",
    sessionLabel: "Session",
    protocolLabel: "Protocol",
    connectionErrorLabel: "Connection error",
    lastFrame: "last message {age} ago",
    lastFrameNever: "no message received yet",
    themeToggle: "Toggle theme",
    languageToggle: "Sprache: Deutsch",
    islandsHeading: "Islands",
    searchPlaceholder: "Search products…",
    colName: "Name",
    colGeneration: "Generation/min",
    colConsumption: "Consumption/min",
    colDelta: "Delta/min",
    colBuildings: "Buildings",
    products: "products",
    deficits: "deficits",
    noProduction: "No production on this island yet.",
    warmingUp: "Waiting for the first statistics tick (up to 2 min)…",
    warmingUpShort: "waiting for statistics…",
    selectIsland: "Select an island to see its production.",
    historyHeading: "History",
    range1h: "1 h",
    range4h: "4 h",
    range24h: "24 h",
    range7d: "7 d",
    rangeSession: "Session",
    historyDisabled: "History is disabled for this run (started with --no-db). Restart without that flag to record history.",
    historyEmpty: "No history yet.",
    historyError: "Could not load the history.",
    legendGeneration: "Generation",
    legendConsumption: "Consumption",
    legendDelta: "Delta",
    efficiencyHeading: "Efficiency",
    efficiencyIntro: "Largest unused production potential first.",
    efficiencyDetails: "How these numbers work",
    efficiencyMeaning: "Actual production divided by potential production at 100 %.",
    productivityMeaning: "Average building productivity; boosts can take it above 100 %.",
    unusedMeaning: "Potential minus actual production, per minute.",
    productivityIdleNote: "not producing",
    productivityIdleMeaning: "Buildings are present but produce nothing right now. The data does not say why.",
    productivityNoBuildingsNote: "no buildings",
    productivityNoBuildingsMeaning: "No buildings for this good on this island; there is no potential to compare.",
    productivityNoPotentialNote: "no potential",
    productivityNoPotentialMeaning: "Buildings are present, but the game reports no production potential to compare.",
    colEfficiency: "Efficiency",
    colProductivity: "Productivity",
    colWasted: "Unused/min",
    colPerfect: "Potential/min",
    tabProducts: "Goods",
    islandSwitch: "Switch island",
    filtersLabel: "Filter goods",
    allGoods: "All",
    onlyDeficits: "Deficits",
    onlyWarnings: "Warnings",
    noMatches: "No matching goods. Try another search or filter.",
    openHistory: "Open history",
    noEfficiencyData: "No production data yet.",
    alertsHeading: "Warnings",
    alertsBadge: "{count} active warnings",
    alertsNone: "No active warnings.",
    alertsHistoryHeading: "Warning history",
    alertsShowHistory: "Show history",
    alertsShowActive: "Show active only",
    alertsHistoryEmpty: "No warnings have been recorded yet.",
    alertsExplain: "A warning is raised when a rule has been true for several measurements in a row, "
      + "not on a single reading. A deficit badge counts goods whose delta is negative right now; "
      + "a warning badge counts sustained problems.",
    colIsland: "Island",
    colProduct: "Product",
    colRule: "Rule",
    colDetail: "Detail",
    colSince: "Since",
    colUntil: "Until",
    ruleDeficit: "Deficit",
    ruleProductivityDrop: "Productivity drop",
    alertActive: "active",
    alertMarker: "Active warning",
    alertSince: "since {age}",
    notificationTitle: "Tabularium 117 warning",
    settingsHeading: "Notifications",
    settingsNotify: "Show a browser notification for new warnings",
    settingsSound: "Play a sound for new warnings",
    settingsExplain: "Both are off by default, are stored in this browser only, and are never sent anywhere.",
    settingsNotifyDenied: "This browser has blocked notifications for this page. Allow them in the "
      + "site settings to use this option.",
    settingsNotifyUnsupported: "This browser does not support notifications.",
    settingsNotifyInsecure: "Notifications are not available over this address: browsers only allow them "
      + "on a secure connection, and phone mode serves plain HTTP inside your own network. On the PC, "
      + "at 127.0.0.1, they work.",
    phoneHeading: "Phone",
    phoneIntro: "Show Tabularium 117 on your phone, on the same network. Nothing leaves your network: "
      + "the phone talks to this PC directly.",
    phoneToggle: "Share with my network (LAN mode)",
    phoneOn: "LAN mode is on:",
    phoneOff: "LAN mode is off. Your data stays on this PC.",
    phoneUnavailable: "LAN mode is not available on this PC:",
    phoneScan: "Scan this code with your phone's camera:",
    phoneQrAlt: "QR code with the address of this Tabularium 117",
    phoneUrl: "Or type this address into the phone's browser:",
    phoneCopy: "Copy",
    phoneCopied: "Copied",
    phoneTokenNote: "The address contains an access token. It is new every time Tabularium 117 starts, "
      + "so an old link stops working - and only devices in your own private network may use it.",
    phoneFirewall: "The first time, Windows asks whether Tabularium 117 may communicate on the network. "
      + "Choose \"Private networks\", not \"Public networks\".",
    phoneError: "LAN mode could not be changed:",
    phoneIndicator: "Phone",
    phoneIndicatorTitle: "Connected over the network to Tabularium 117 on the PC",
    screenHeading: "Screen",
    wakeLock: "Keep the screen on",
    wakeLockExplain: "Stops the phone from locking while Tabularium 117 is open. It is off by default, "
      + "stored in this browser only, and the browser may still switch it off (for example in "
      + "battery saver mode).",
    wakeLockUnsupported: "This browser cannot keep the screen on.",
    wakeLockInsecure: "Keeping the screen on is not available over this address: browsers only allow it "
      + "on a secure connection, and phone mode serves plain HTTP inside your own network.",
    helpHeading: "Help",
    helpPipeIntro: "Tabularium 117 reads Anno 117's unofficial pipe interface. To enable it:",
    helpPipeStep1: "Steam: in your library, right-click Anno 117 → Properties → General, "
      + "and enter /pipe under \"Launch Options\".",
    helpPipeStep2: "Or Ubisoft Connect: right-click Anno 117 → Properties → \"Add launch "
      + "arguments\", and enter /pipe.",
    helpPipeStep3: "Start the game and load a savegame. Tabularium 117 connects automatically and "
      + "reconnects if the game restarts or loads a different savegame.",
    helpLocal: "All data stays on this PC unless you explicitly enable LAN mode under "
      + "\"Phone\"; even then it only travels inside your own network. There is no cloud "
      + "service, no account and no telemetry.",
    helpNumbers: "Generation and consumption are per minute. Delta is generation minus "
      + "consumption; a negative delta (shown in red) means the island is running a deficit.",
    helpUnknownGuid: "An entry shown as \"#123456\" is a game object Tabularium 117's catalog does not "
      + "know yet. It is not an error - the number is the game's own identifier.",
    helpVersion: "Tabularium 117 {version}",
    helpBack: "Back",
    errorPrefix: "Error:",
    connectionLost: "Connection to Tabularium 117 lost - retrying…",
  },
  de: {
    appName: "Tabularium 117",
    statusConnected: "verbunden",
    statusWaiting: "warte auf Anno 117",
    statusDisconnected: "getrennt",
    statusReplaying: "Wiedergabe",
    statusEnded: "Aufnahme beendet",
    connectionDetails: "Details",
    sourceLabel: "Quelle",
    sourcePipe: "Anno 117",
    sourceReplay: "Aufnahme",
    sessionLabel: "Sitzung",
    protocolLabel: "Protokoll",
    connectionErrorLabel: "Verbindungsfehler",
    lastFrame: "letzte Meldung vor {age}",
    lastFrameNever: "noch keine Meldung empfangen",
    themeToggle: "Design umschalten",
    languageToggle: "Language: English",
    islandsHeading: "Inseln",
    searchPlaceholder: "Waren suchen…",
    colName: "Name",
    colGeneration: "Produktion/min",
    colConsumption: "Verbrauch/min",
    colDelta: "Delta/min",
    colBuildings: "Gebäude",
    products: "Waren",
    deficits: "Defizite",
    noProduction: "Auf dieser Insel gibt es noch keine Produktion.",
    warmingUp: "Warte auf den ersten Statistik-Tick (bis zu 2 min) …",
    warmingUpShort: "wartet auf Statistik …",
    selectIsland: "Insel auswählen, um die Produktion zu sehen.",
    historyHeading: "Verlauf",
    range1h: "1 Std",
    range4h: "4 Std",
    range24h: "24 Std",
    range7d: "7 Tage",
    rangeSession: "Sitzung",
    historyDisabled: "Der Verlauf ist für diesen Lauf deaktiviert (mit --no-db gestartet). "
      + "Ohne dieses Flag neu starten, um den Verlauf aufzuzeichnen.",
    historyEmpty: "Noch kein Verlauf vorhanden.",
    historyError: "Verlauf konnte nicht geladen werden.",
    legendGeneration: "Produktion",
    legendConsumption: "Verbrauch",
    legendDelta: "Delta",
    efficiencyHeading: "Effizienz",
    efficiencyIntro: "Waren mit dem größten ungenutzten Produktionspotenzial zuerst.",
    efficiencyDetails: "Kennzahlen verstehen",
    efficiencyMeaning: "Tatsächliche Produktion geteilt durch das Potenzial bei 100 %.",
    productivityMeaning: "Durchschnittliche Gebäudeproduktivität; Boni können sie über 100 % heben.",
    unusedMeaning: "Potenzial minus tatsächliche Produktion, pro Minute.",
    productivityIdleNote: "produziert nicht",
    productivityIdleMeaning: "Gebäude sind vorhanden, produzieren aber gerade nichts. Den Grund liefern die Daten nicht.",
    productivityNoBuildingsNote: "keine Gebäude",
    productivityNoBuildingsMeaning: "Für diese Ware gibt es hier keine Gebäude und somit kein vergleichbares Potenzial.",
    productivityNoPotentialNote: "kein Potenzial",
    productivityNoPotentialMeaning: "Gebäude sind vorhanden, aber das Spiel meldet kein vergleichbares Produktionspotenzial.",
    colEfficiency: "Effizienz",
    colProductivity: "Produktivität",
    colWasted: "Ungenutzt/min",
    colPerfect: "Potenzial/min",
    tabProducts: "Waren",
    islandSwitch: "Insel wechseln",
    filtersLabel: "Waren filtern",
    allGoods: "Alle",
    onlyDeficits: "Defizite",
    onlyWarnings: "Warnungen",
    noMatches: "Keine passenden Waren. Ändere die Suche oder den Filter.",
    openHistory: "Verlauf öffnen",
    noEfficiencyData: "Noch keine Produktionsdaten.",
    alertsHeading: "Warnungen",
    alertsBadge: "{count} aktive Warnungen",
    alertsNone: "Keine aktiven Warnungen.",
    alertsHistoryHeading: "Warnungsverlauf",
    alertsShowHistory: "Verlauf anzeigen",
    alertsShowActive: "Nur aktive anzeigen",
    alertsHistoryEmpty: "Es wurden noch keine Warnungen aufgezeichnet.",
    alertsExplain: "Eine Warnung entsteht, wenn eine Regel über mehrere Messungen hinweg zutrifft, "
      + "nicht bei einem einzelnen Messwert. Das Defizit-Abzeichen zählt Waren mit aktuell negativem "
      + "Delta, das Warnungs-Abzeichen zählt anhaltende Probleme.",
    colIsland: "Insel",
    colProduct: "Ware",
    colRule: "Regel",
    colDetail: "Details",
    colSince: "Seit",
    colUntil: "Bis",
    ruleDeficit: "Defizit",
    ruleProductivityDrop: "Produktivitätseinbruch",
    alertActive: "aktiv",
    alertMarker: "Aktive Warnung",
    alertSince: "seit {age}",
    notificationTitle: "Tabularium-117-Warnung",
    settingsHeading: "Benachrichtigungen",
    settingsNotify: "Browser-Benachrichtigung bei neuen Warnungen anzeigen",
    settingsSound: "Ton bei neuen Warnungen abspielen",
    settingsExplain: "Beides ist standardmäßig aus, wird nur in diesem Browser gespeichert und "
      + "niemals irgendwohin gesendet.",
    settingsNotifyDenied: "Dieser Browser hat Benachrichtigungen für diese Seite blockiert. Sie "
      + "müssen in den Website-Einstellungen erlaubt werden, damit diese Option funktioniert.",
    settingsNotifyUnsupported: "Dieser Browser unterstützt keine Benachrichtigungen.",
    settingsNotifyInsecure: "Benachrichtigungen sind über diese Adresse nicht verfügbar: Browser erlauben "
      + "sie nur über eine sichere Verbindung, und der Handy-Modus liefert einfaches HTTP im eigenen "
      + "Netzwerk aus. Auf dem PC, unter 127.0.0.1, funktionieren sie.",
    phoneHeading: "Handy",
    phoneIntro: "Tabularium 117 auf dem Handy im selben Netzwerk anzeigen. Nichts verlässt das eigene "
      + "Netzwerk: Das Handy spricht direkt mit diesem PC.",
    phoneToggle: "Für mein Netzwerk freigeben (LAN-Modus)",
    phoneOn: "LAN-Modus ist an:",
    phoneOff: "LAN-Modus ist aus. Die Daten bleiben auf diesem PC.",
    phoneUnavailable: "LAN-Modus ist auf diesem PC nicht möglich:",
    phoneScan: "Diesen Code mit der Handykamera scannen:",
    phoneQrAlt: "QR-Code mit der Adresse dieses Tabularium 117",
    phoneUrl: "Oder diese Adresse im Browser des Handys eingeben:",
    phoneCopy: "Kopieren",
    phoneCopied: "Kopiert",
    phoneTokenNote: "Die Adresse enthält einen Zugriffscode. Er ist bei jedem Start von Tabularium 117 "
      + "neu, ein alter Link funktioniert also nicht mehr - und nur Geräte im eigenen privaten "
      + "Netzwerk dürfen ihn überhaupt verwenden.",
    phoneFirewall: "Beim ersten Mal fragt Windows, ob Tabularium 117 im Netzwerk kommunizieren darf. "
      + "Dort „Private Netzwerke“ wählen, nicht „Öffentliche Netzwerke“.",
    phoneError: "LAN-Modus konnte nicht geändert werden:",
    phoneIndicator: "Handy",
    phoneIndicatorTitle: "Über das Netzwerk mit Tabularium 117 auf dem PC verbunden",
    screenHeading: "Bildschirm",
    wakeLock: "Bildschirm anlassen",
    wakeLockExplain: "Verhindert, dass sich das Handy sperrt, solange Tabularium 117 geöffnet ist. "
      + "Standardmäßig aus, nur in diesem Browser gespeichert; der Browser kann die Sperre "
      + "trotzdem aufheben (zum Beispiel im Energiesparmodus).",
    wakeLockUnsupported: "Dieser Browser kann den Bildschirm nicht anlassen.",
    wakeLockInsecure: "Den Bildschirm anzulassen ist über diese Adresse nicht verfügbar: Browser erlauben "
      + "es nur über eine sichere Verbindung, und der Handy-Modus liefert einfaches HTTP im eigenen "
      + "Netzwerk aus.",
    helpHeading: "Hilfe",
    helpPipeIntro: "Tabularium 117 liest die inoffizielle Pipe-Schnittstelle von Anno 117. So wird sie aktiviert:",
    helpPipeStep1: "Steam: in der Bibliothek Rechtsklick auf Anno 117 → Eigenschaften → "
      + "Allgemein, und unter „Startoptionen“ /pipe eintragen.",
    helpPipeStep2: "Oder Ubisoft Connect: Rechtsklick auf Anno 117 → Eigenschaften → "
      + "„Startargumente hinzufügen“, und /pipe eintragen.",
    helpPipeStep3: "Das Spiel starten und einen Spielstand laden. Tabularium 117 verbindet sich "
      + "automatisch und stellt die Verbindung wieder her, wenn das Spiel neu startet oder ein "
      + "anderer Spielstand geladen wird.",
    helpLocal: "Alle Daten bleiben auf diesem PC, solange der LAN-Modus unter „Handy“ nicht "
      + "ausdrücklich aktiviert wird; auch dann bleiben sie im eigenen Netzwerk. Es gibt keinen "
      + "Cloud-Dienst, kein Konto und keine Telemetrie.",
    helpNumbers: "Produktion und Verbrauch sind pro Minute. Delta ist Produktion minus "
      + "Verbrauch; ein negatives Delta (rot dargestellt) bedeutet ein Defizit auf der Insel.",
    helpUnknownGuid: "Ein Eintrag wie „#123456“ ist ein Spielobjekt, das der Katalog von "
      + "Tabularium 117 noch nicht kennt. Das ist kein Fehler - die Zahl ist die eigene Kennung des Spiels.",
    helpVersion: "Tabularium 117 {version}",
    helpBack: "Zurück",
    errorPrefix: "Fehler:",
    connectionLost: "Verbindung zu Tabularium 117 verloren - erneuter Versuch…",
  },
};

// apiLangCodes maps the UI's ISO code to the catalog's language code that
// the API's ?lang= parameter expects (KONZEPT.md section 5).
const apiLangCodes = { de: "german", en: "english" };

function detectLanguage() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "de" || stored === "en") return stored;
  } catch {
    // Storage may be unavailable (private browsing); fall through to detection.
  }
  const nav = (navigator.language || "en").toLowerCase();
  return nav.startsWith("de") ? "de" : "en";
}

export const i18n = {
  lang: detectLanguage(),

  t(key, vars) {
    const dict = dictionaries[this.lang] || dictionaries.en;
    let text = dict[key] ?? dictionaries.en[key] ?? key;
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        text = text.replaceAll(`{${k}}`, v);
      }
    }
    return text;
  },

  setLang(lang) {
    if (lang !== "de" && lang !== "en") return;
    this.lang = lang;
    try {
      localStorage.setItem(STORAGE_KEY, lang);
    } catch {
      // Ignore: the choice just does not survive a reload.
    }
    document.documentElement.lang = lang;
    this.apply();
  },

  toggle() {
    this.setLang(this.lang === "de" ? "en" : "de");
  },

  // apiLang is what the API's ?lang= parameter expects for the current UI language.
  apiLang() {
    return apiLangCodes[this.lang] || "english";
  },

  // apply updates every element with [data-i18n] / [data-i18n-placeholder]
  // in place, and the document title.
  apply() {
    document.documentElement.lang = this.lang;
    document.title = this.t("appName");
    for (const el of document.querySelectorAll("[data-i18n]")) {
      el.textContent = this.t(el.getAttribute("data-i18n"));
    }
    for (const el of document.querySelectorAll("[data-i18n-placeholder]")) {
      el.placeholder = this.t(el.getAttribute("data-i18n-placeholder"));
    }
    for (const el of document.querySelectorAll("[data-i18n-title]")) {
      el.title = this.t(el.getAttribute("data-i18n-title"));
    }
    document.dispatchEvent(new CustomEvent("tabularium-lang-changed"));
  },
};
