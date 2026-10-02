import type { TFunction } from "i18next";
import { DIRECT_PROXY_ID, type Proxy } from "../../api/backend";

/** A proxy's name; the built-in direct entry has none of its own and is named in the user's language. */
export function proxyName(t: TFunction, p: Pick<Proxy, "id" | "name">): string {
  return p.id === DIRECT_PROXY_ID ? t("proxies.direct") : p.name;
}

/** The kinds the proxy editor offers so far (the V2Ray family follows in M6). */
export const editableKinds = ["socks", "http"] as const;

/** The name of a proxy kind, such as "SOCKS5". */
export function kindName(t: TFunction, kind: string): string {
  return t(`proxies.kinds.${kind}`, { defaultValue: kind });
}
