import type { ProfileView, SessionView } from "../../api/backend";
import { isActive } from "../connections/status";

/** Who uses a proxy: every profile set to it, and those of them connected now. */
export interface ProxyUsage {
  users: ProfileView[];
  connected: ProfileView[];
}

export function proxyUsage(proxyId: string, profiles: ProfileView[], sessions: Record<string, SessionView>): ProxyUsage {
  const users = profiles.filter((p) => p.profile.proxyId === proxyId);
  return { users, connected: users.filter((p) => isActive(sessions[p.profile.id])) };
}

/** The profiles' names for a sentence, in list order. */
export function profileNames(list: ProfileView[], separator: string): string {
  return list.map((p) => p.profile.name).join(separator);
}
