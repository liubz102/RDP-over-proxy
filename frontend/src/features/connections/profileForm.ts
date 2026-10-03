import { RDP_PORT, type Profile } from "../../api/backend";
import { joinHostPort, splitHostPort } from "../../lib/address";

/** Which monitors a full-screen session uses. */
export type Screens = "one" | "multimon" | "span";

/** The profile editor's fields, as the user types them. */
export interface ProfileForm {
  name: string;
  group: string;
  /** host, host:port, [IPv6]:port */
  address: string;
  username: string;
  password: string;
  rememberPassword: boolean;
  mode: string;
  width: string;
  height: string;
  screens: Screens;
  admin: boolean;
}

export function toForm(p: Profile): ProfileForm {
  return {
    name: p.name,
    group: p.group,
    address: joinHostPort(p.target.host, p.target.port, RDP_PORT),
    username: p.username,
    password: "",
    rememberPassword: p.rememberPassword,
    mode: p.display.mode,
    width: String(p.display.width),
    height: String(p.display.height),
    screens: p.display.span ? "span" : p.display.multimon ? "multimon" : "one",
    admin: p.admin,
  };
}

function toInt(text: string): number {
  return /^\s*\d+\s*$/.test(text) ? Number(text) : 0;
}

/**
 * The profile to save: base with the form applied, and the problems the Go
 * side cannot see (it gets numbers, not the text typed). Field names are the
 * ones the Go side reports, so both kinds of problem land on the same field.
 * The proxy is base's: it is chosen in the list (ProfileService.SetProxy),
 * and Update keeps the stored one.
 */
export function fromForm(base: Profile, f: ProfileForm): { profile: Profile; problems: Record<string, string> } {
  const problems: Record<string, string> = {};
  const target = splitHostPort(f.address);
  if (Number.isNaN(target.port)) problems["target.port"] = "invalid";
  // The password is only sent when it is to be remembered.
  if (f.rememberPassword && f.password !== "" && f.username.trim() === "") problems.username = "requiredForPassword";
  const profile: Profile = {
    ...base,
    name: f.name,
    group: f.group,
    target: { host: target.host, port: Number.isNaN(target.port) || target.port === 0 ? RDP_PORT : target.port },
    username: f.username,
    rememberPassword: f.rememberPassword,
    display: {
      ...base.display,
      mode: f.mode,
      // Kept while another mode is chosen, so switching back restores them.
      width: f.mode === "window" ? toInt(f.width) : base.display.width,
      height: f.mode === "window" ? toInt(f.height) : base.display.height,
      multimon: f.mode === "fullscreen" ? f.screens === "multimon" : base.display.multimon,
      span: f.mode === "fullscreen" ? f.screens === "span" : base.display.span,
    },
    admin: f.admin,
  };
  return { profile, problems };
}

/** The form field a problem the Go side reported belongs to. */
export function formField(field: string): keyof ProfileForm | null {
  switch (field) {
    case "name":
    case "group":
    case "username":
    case "password":
      return field;
    case "target.host":
    case "target.port":
      return "address";
    case "display.width":
      return "width";
    case "display.height":
      return "height";
    case "display.span":
    case "display.multimon":
      return "screens";
    case "display.mode":
      return "mode";
    default:
      return null;
  }
}
