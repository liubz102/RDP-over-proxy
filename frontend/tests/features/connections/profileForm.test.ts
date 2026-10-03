import { describe, expect, it } from "vitest";
import type { Profile } from "../../../src/api/backend";
import { formField, fromForm, toForm } from "../../../src/features/connections/profileForm";

const profile: Profile = {
  schema: 1,
  id: "0123456789abcdef",
  name: "Office PC",
  group: "Work",
  target: { host: "pc.example.com", port: 3389 },
  proxyId: "fedcba9876543210",
  loopback: "127.1.2.3",
  username: "EXAMPLE\\alice",
  rememberPassword: true,
  display: { mode: "default", width: 1280, height: 800, multimon: false, span: false },
  admin: false,
};

describe("profile form", () => {
  it("round-trips a profile unchanged", () => {
    const { profile: back, problems } = fromForm(profile, toForm(profile));
    expect(back).toEqual(profile);
    expect(problems).toEqual({});
  });

  it("splits a pasted host:port", () => {
    const f = { ...toForm(profile), address: "192.0.2.10:3390" };
    expect(fromForm(profile, f).profile.target).toEqual({ host: "192.0.2.10", port: 3390 });
    expect(toForm(fromForm(profile, f).profile).address).toBe("192.0.2.10:3390");
  });

  it("reports a port that is not a number", () => {
    const f = { ...toForm(profile), address: "pc.example.com:rdp" };
    expect(fromForm(profile, f).problems).toEqual({ "target.port": "invalid" });
  });

  it("wants a user name for a password", () => {
    const f = { ...toForm(profile), username: " ", password: "secret" };
    expect(fromForm(profile, f).problems).toEqual({ username: "requiredForPassword" });
    expect(fromForm(profile, { ...f, rememberPassword: false }).problems).toEqual({});
  });

  it("keeps the settings of the display modes not chosen", () => {
    const windowed = { ...profile, display: { ...profile.display, mode: "window", width: 1600, height: 900 } };
    const f = { ...toForm(windowed), mode: "fullscreen", width: "", screens: "span" as const };
    const d = fromForm(windowed, f).profile.display;
    expect(d).toEqual({ mode: "fullscreen", width: 1600, height: 900, multimon: false, span: true });
    expect(toForm(fromForm(windowed, f).profile).screens).toBe("span");
  });

  it("sends a window size that is not a number as 0, for the Go side to reject", () => {
    const f = { ...toForm(profile), mode: "window", width: "wide", height: "900" };
    expect(fromForm(profile, f).profile.display).toMatchObject({ width: 0, height: 900 });
  });

  it("maps the Go side's field paths to form fields", () => {
    expect(formField("target.host")).toBe("address");
    expect(formField("display.span")).toBe("screens");
    expect(formField("loopback")).toBeNull();
  });
});
