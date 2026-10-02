import { useSyncExternalStore } from "react";
import { webDarkTheme, webLightTheme, type Theme } from "@fluentui/react-components";

// Segoe UI has no CJK glyphs; naming Microsoft YaHei UI explicitly keeps
// Chinese text in the Windows UI font instead of whatever fallback the
// WebView picks.
const fontFamilyBase =
  '"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", "Microsoft YaHei", sans-serif';

export const lightTheme: Theme = { ...webLightTheme, fontFamilyBase };
export const darkTheme: Theme = { ...webDarkTheme, fontFamilyBase };

const darkQuery = "(prefers-color-scheme: dark)";

function subscribe(onChange: () => void) {
  const mql = window.matchMedia(darkQuery);
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
}

/** Whether Windows is in dark mode; re-renders when the user switches it. */
export function useSystemDark(): boolean {
  return useSyncExternalStore(subscribe, () => window.matchMedia(darkQuery).matches);
}
