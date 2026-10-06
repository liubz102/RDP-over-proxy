import { describe, expect, it } from "vitest";
import type { LocalCandidate } from "../../../src/api/backend";
import { addOffer, offerName, offerOf, proxyOf, type LocalOffer } from "../../../src/features/proxies/localOffers";

const program = (port: number, name = "v2rayN"): LocalCandidate => ({
  name,
  hosts: ["127.0.0.1", "::1"],
  port,
  source: "program",
});
const system: LocalCandidate = { name: "Fiddler", hosts: ["127.0.0.1"], port: 8888, source: "system" };

describe("offerOf", () => {
  it("offers a program's port once it answered as SOCKS5, at the address that answered", () => {
    expect(offerOf(0, program(10808))).toBeNull(); // not asked yet
    expect(offerOf(0, program(10808), { socks5: false, password: false, host: "127.0.0.1" })).toBeNull();
    expect(offerOf(0, program(10808), { socks5: true, password: false, host: "::1" })).toEqual({
      order: 0,
      program: "v2rayN",
      host: "::1",
      port: 10808,
      kind: "socks",
      password: false,
      system: false,
    });
    expect(offerOf(1, program(1080), { socks5: true, password: true, host: "127.0.0.1" })?.password).toBe(true);
  });

  it("offers the proxy in Windows' settings as HTTP, without asking", () => {
    expect(offerOf(2, system)).toEqual({
      order: 2,
      program: "Fiddler",
      host: "127.0.0.1",
      port: 8888,
      kind: "http",
      password: false,
      system: true,
    });
    expect(offerOf(2, { ...system, hosts: null })).toBeNull();
  });
});

describe("addOffer", () => {
  const socks = (order: number, port: number) =>
    offerOf(order, program(port), { socks5: true, password: false, host: "127.0.0.1" }) as LocalOffer;

  it("keeps the Go side's order whatever order the answers come in", () => {
    let offers = addOffer([], socks(1, 10808));
    offers = addOffer(offers, null);
    offers = addOffer(offers, socks(0, 2080));
    expect(offers.map((o) => o.port)).toEqual([2080, 10808]);
  });

  it("offers an address once", () => {
    const offers = addOffer([socks(0, 10808)], socks(3, 10808));
    expect(offers).toHaveLength(1);
  });
});

describe("offerName and proxyOf", () => {
  const named = (p: string) => (p ? `Local ${p}` : "Local proxy");
  const a = offerOf(0, program(7890, "Clash Verge"), { socks5: true, password: false, host: "127.0.0.1" }) as LocalOffer;
  const b = offerOf(1, program(7891, "Clash Verge"), { socks5: true, password: false, host: "127.0.0.1" }) as LocalOffer;
  const c = offerOf(2, { ...system, name: "" }) as LocalOffer;

  it("names the proxy after its program, with the port when the program has two", () => {
    expect(offerName(a, [a], named)).toBe("Local Clash Verge");
    expect(offerName(a, [a, b, c], named)).toBe("Local Clash Verge 7890");
    expect(offerName(c, [a, b, c], named)).toBe("Local proxy");
  });

  it("stores the address and kind, with no account", () => {
    const p = proxyOf(c, "Local proxy");
    expect(p).toMatchObject({ id: "", name: "Local proxy", kind: "http", server: "127.0.0.1", port: 8888, username: "", secret: "" });
  });
});
