// Showing the environment report (internal/diag): every item is a key with
// either a value to translate ("diag.items.<key>.<value>") or text to show
// as it is, and maybe a detail line.
import type { TFunction } from "i18next";
import type { DiagItem } from "../../api/backend";

/** The report's items in their groups, in the order the Go side lists them. */
export function byGroup(items: DiagItem[]): [string, DiagItem[]][] {
  const groups = new Map<string, DiagItem[]>();
  for (const it of items) {
    const list = groups.get(it.group);
    if (list) list.push(it);
    else groups.set(it.group, [it]);
  }
  return [...groups];
}

/** What an item says. */
export function valueText(t: TFunction, it: DiagItem): string {
  return it.value ? t(`diag.items.${it.key}.${it.value}`, { ...it.args }) : (it.text ?? "");
}

/** An item's detail line, or "" when it has none. */
export function detailText(t: TFunction, it: DiagItem): string {
  return it.detail ? t(`diag.items.${it.key}.detail`, { text: it.detail }) : "";
}

// Marks for items that may get in the way, so a reader of a copy finds them.
const marks: Record<string, string> = { warn: "[!] ", error: "[x] " };

/**
 * The report as plain text, for a bug report. Private details (the names of
 * servers on the user's network, folders) are left out.
 */
export function reportText(t: TFunction, items: DiagItem[]): string {
  const lines = [t("diag.copyTitle")];
  for (const [group, list] of byGroup(items)) {
    lines.push("", `[${t(`diag.groups.${group}`)}]`);
    for (const it of list) {
      lines.push(`${marks[it.status] ?? ""}${t(`diag.items.${it.key}.label`)}${t("common.colon")}${valueText(t, it)}`);
      if (it.detail && !it.private) lines.push(`    ${detailText(t, it)}`);
    }
  }
  return lines.join("\n");
}
