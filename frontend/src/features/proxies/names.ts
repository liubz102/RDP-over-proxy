import type { TFunction } from "i18next";
import { DIRECT_PROXY_ID, type Proxy } from "../../api/backend";

/** A proxy's name; the built-in direct entry has none of its own and is named in the user's language. */
export function proxyName(t: TFunction, p: Pick<Proxy, "id" | "name">): string {
  return p.id === DIRECT_PROXY_ID ? t("proxies.direct") : p.name;
}

/** The kinds the proxy editor offers, in order (model.Kinds). */
export const editableKinds = ["socks", "http", "shadowsocks", "vmess", "vless", "trojan", "hysteria2", "xray"] as const;

// The choices of the editor's lists, as the Go side names them (internal/model/options.go).
export const networks = ["tcp", "ws", "grpc", "xhttp", "httpupgrade", "kcp"] as const;
export const securities = ["none", "tls", "reality"] as const;
export const vmessCiphers = ["auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero"] as const;
export const shadowsocksCiphers = [
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
  "aes-128-gcm",
  "aes-256-gcm",
  "chacha20-poly1305",
  "xchacha20-poly1305",
  "none",
] as const;
export const vlessFlows = ["", "xtls-rprx-vision", "xtls-rprx-vision-udp443"] as const;
export const tcpHeaders = ["none", "http"] as const;
export const kcpHeaders = ["none", "srtp", "utp", "wechat-video", "dtls", "wireguard", "dns"] as const;
export const grpcModes = ["gun", "multi"] as const;
export const xhttpModes = ["auto", "packet-up", "stream-up", "stream-one"] as const;
export const fingerprints = ["chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"] as const;

/** The name of a proxy kind, such as "SOCKS5". */
export function kindName(t: TFunction, kind: string): string {
  return t(`proxies.kinds.${kind}`, { defaultValue: kind });
}

/** The name of a transport, such as "WebSocket". */
export function networkName(t: TFunction, network: string): string {
  return t(`proxies.networks.${network}`, { defaultValue: network });
}

/** The name of a security layer, such as "REALITY". */
export function securityName(t: TFunction, security: string): string {
  return t(`proxies.securities.${security}`, { defaultValue: security });
}

/**
 * How a proxy's transport reads in a list: "WebSocket + TLS", "REALITY",
 * "TLS"; empty when there is nothing to say (plain TCP, or no transport).
 */
export function transportName(t: TFunction, network: string, security: string): string {
  const parts: string[] = [];
  if (network !== "" && network !== "tcp") parts.push(networkName(t, network));
  if (security !== "" && security !== "none") parts.push(securityName(t, security));
  return parts.join(" + ");
}
