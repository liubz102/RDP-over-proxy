import { Clipboard } from "@wailsio/runtime";

/** Puts text on the clipboard: the page's own way first, Wails' when the page may not. */
export async function copyText(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    await Clipboard.SetText(text);
  }
}
