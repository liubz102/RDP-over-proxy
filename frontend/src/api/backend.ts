// One place to import the generated Wails bindings from, so the long generated
// paths stay out of the rest of the code.
export { SettingsService } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/api";
export type { AppInfo } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/api";
export type { Settings } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/model";

// Event names emitted by the Go side (internal/api).
export const Events = {
  settingsChanged: "settings:changed",
} as const;
