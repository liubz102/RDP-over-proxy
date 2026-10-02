import { describe, expect, it } from "vitest";
import { joinHostPort, splitHostPort } from "./address";

describe("splitHostPort", () => {
  it.each([
    ["pc.example.com", "pc.example.com", 0],
    [" pc.example.com:3390 ", "pc.example.com", 3390],
    ["192.0.2.10:3389", "192.0.2.10", 3389],
    ["[2001:db8::1]:3390", "2001:db8::1", 3390],
    ["[2001:db8::1]", "2001:db8::1", 0],
    ["2001:db8::1", "2001:db8::1", 0],
    ["", "", 0],
  ])("%j → %j, %j", (text, host, port) => {
    expect(splitHostPort(text)).toEqual({ host, port });
  });

  it("marks a port that is not a number", () => {
    expect(splitHostPort("pc.example.com:rdp").port).toBeNaN();
    expect(splitHostPort("pc.example.com:").port).toBeNaN();
    expect(splitHostPort("[2001:db8::1]:x").port).toBeNaN();
  });

  it("leaves text after a closing bracket that is not a port in the host, for the Go side to reject", () => {
    expect(splitHostPort("[2001:db8::1]x")).toEqual({ host: "[2001:db8::1]x", port: 0 });
  });
});

describe("joinHostPort", () => {
  it("leaves out the default port", () => {
    expect(joinHostPort("pc.example.com", 3389, 3389)).toBe("pc.example.com");
    expect(joinHostPort("2001:db8::1", 3389, 3389)).toBe("2001:db8::1");
  });

  it("adds any other port, with brackets around IPv6", () => {
    expect(joinHostPort("pc.example.com", 3390, 3389)).toBe("pc.example.com:3390");
    expect(joinHostPort("2001:db8::1", 3390, 3389)).toBe("[2001:db8::1]:3390");
  });

  it("round-trips through splitHostPort", () => {
    for (const [host, port] of [
      ["pc.example.com", 3390],
      ["2001:db8::1", 443],
      ["192.0.2.10", 3389],
    ] as const) {
      const back = splitHostPort(joinHostPort(host, port, 3389));
      expect(back.host).toBe(host);
      expect(back.port === 0 ? 3389 : back.port).toBe(port);
    }
  });
});
