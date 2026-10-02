import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import zhCN from "./locales/zh-CN.json";
import en from "./locales/en.json";

export const languages = ["zh-CN", "en"] as const;
export type Language = (typeof languages)[number];

export function isLanguage(value: string): value is Language {
  return (languages as readonly string[]).includes(value);
}

// The language is chosen by the Go side (settings.json, or the system language
// before the user has picked one); see stores/settings.ts. Until that answer
// arrives, follow the WebView's own language so the loading text doesn't flash
// in the wrong language.
const initial: Language = navigator.language.toLowerCase().startsWith("zh") ? "zh-CN" : "en";

void i18n.use(initReactI18next).init({
  resources: {
    "zh-CN": { translation: zhCN },
    en: { translation: en },
  },
  lng: initial,
  fallbackLng: "en",
  interpolation: { escapeValue: false },
  returnNull: false,
});

export function applyLanguage(lang: Language) {
  document.documentElement.lang = lang;
  if (i18n.language !== lang) {
    void i18n.changeLanguage(lang);
  }
}

export default i18n;
