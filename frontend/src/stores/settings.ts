import { create } from "zustand";
import { Events as WailsEvents } from "@wailsio/runtime";
import { Events, SettingsService, type AppInfo, type Settings } from "../api/backend";
import { applyLanguage, isLanguage, type Language } from "../i18n";

type Status = "loading" | "ready" | "error";

interface SettingsState {
  status: Status;
  loadError: string | null;
  saveError: string | null;
  settings: Settings | null;
  /** The Windows display language, used until the user picks one. */
  systemLanguage: Language;
  appInfo: AppInfo | null;
  load: () => Promise<void>;
  save: (patch: Partial<Settings>) => Promise<void>;
}

/** The language the UI is shown in: the user's choice, or the system language before they choose. */
export function uiLanguage(settings: Settings | null, systemLanguage: Language): Language {
  return settings && isLanguage(settings.language) ? settings.language : systemLanguage;
}

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export const useSettings = create<SettingsState>((set, get) => ({
  status: "loading",
  loadError: null,
  saveError: null,
  settings: null,
  systemLanguage: "en",
  appInfo: null,

  async load() {
    try {
      const [settings, system, appInfo] = await Promise.all([
        SettingsService.Get(),
        SettingsService.SystemLanguage(),
        SettingsService.AppInfo(),
      ]);
      const systemLanguage = isLanguage(system) ? system : "en";
      applyLanguage(uiLanguage(settings, systemLanguage));
      set({ status: "ready", settings, systemLanguage, appInfo, loadError: null });
    } catch (e) {
      set({ status: "error", loadError: errorMessage(e) });
    }
  },

  async save(patch) {
    const current = get().settings;
    if (!current) return;
    try {
      const saved = await SettingsService.Save({ ...current, ...patch });
      applySettings(saved);
      set({ saveError: null });
    } catch (e) {
      set({ saveError: errorMessage(e) });
    }
  },
}));

function applySettings(settings: Settings) {
  applyLanguage(uiLanguage(settings, useSettings.getState().systemLanguage));
  useSettings.setState({ settings });
}

// Settings saved from anywhere (this window, the tray, another window) are
// broadcast by the Go side; this keeps every view in step without polling.
WailsEvents.On(Events.settingsChanged, (event) => applySettings(event.data));
