import { create } from "zustand";
import { Events as WailsEvents } from "@wailsio/runtime";
import {
  AppService,
  Events,
  ProfileService,
  ProxyService,
  SessionService,
  type DataView,
  type LogLine,
  type Notice,
  type ProfileView,
  type ProxyView,
  type SessionView,
} from "../api/backend";
import { appendLog, mergeLog } from "../lib/sessionLog";

type Status = "loading" | "ready" | "error";

interface DataState {
  status: Status;
  loadError: string | null;
  profiles: ProfileView[];
  proxies: ProxyView[];
  /** The latest state of each profile's session, ended ones included. */
  sessions: Record<string, SessionView>;
  /** Each profile's latest session log, once it has been read or has grown. */
  logs: Record<string, LogLine[]>;
  notices: Notice[];
  /** Set while the app asks the user to confirm quitting: how many sessions it would end. */
  quitConfirm: number | null;
  load: () => Promise<void>;
  loadLog: (profileId: string) => Promise<void>;
  dismissNotice: (id: number) => void;
  /** Quit, asking first when remote desktops are connected. */
  quit: () => Promise<void>;
  confirmQuit: (yes: boolean) => void;
}

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * What events changed while load() was waiting for its reads. The replies
 * and the events travel separately, so an event can carry newer news than
 * a reply that lands after it; load() keeps what the events said.
 */
interface Changes {
  data: boolean;
  sessions: Set<string>;
}
/** One per load() in flight (StrictMode runs the first one twice). */
const loading = new Set<Changes>();

export const useData = create<DataState>((set) => ({
  status: "loading",
  loadError: null,
  profiles: [],
  proxies: [],
  sessions: {},
  logs: {},
  notices: [],
  quitConfirm: null,

  async load() {
    const changed: Changes = { data: false, sessions: new Set() };
    loading.add(changed);
    try {
      const [profiles, proxies, states, notices] = await Promise.all([
        ProfileService.List(),
        ProxyService.List(),
        SessionService.States(),
        AppService.Notices(),
      ]);
      set((s) => {
        const sessions: Record<string, SessionView> = {};
        for (const v of states ?? []) sessions[v.profileId] = v;
        for (const id of changed.sessions) sessions[id] = s.sessions[id];
        const read = notices ?? [];
        const later = s.notices.filter((n) => !read.some((r) => r.id === n.id));
        return {
          status: "ready",
          loadError: null,
          ...(changed.data ? {} : { profiles: profiles ?? [], proxies: proxies ?? [] }),
          sessions,
          notices: [...read, ...later],
        };
      });
    } catch (e) {
      set({ status: "error", loadError: errorMessage(e) });
    } finally {
      loading.delete(changed);
    }
  },

  async loadLog(profileId) {
    try {
      const read = (await SessionService.Log(profileId)) ?? [];
      set((s) => ({ logs: { ...s.logs, [profileId]: mergeLog(read, s.logs[profileId]) } }));
    } catch (e) {
      // The lines sent as events are still shown.
      console.error("read the session log", e);
    }
  },

  dismissNotice(id) {
    set((s) => ({ notices: s.notices.filter((n) => n.id !== id) }));
    AppService.Dismiss(id).catch((e: unknown) => console.error("dismiss a notice", e));
  },

  async quit() {
    const v = await AppService.Quit(false);
    if (v.connected > 0) set({ quitConfirm: v.connected });
  },

  confirmQuit(yes) {
    set({ quitConfirm: null });
    const call = yes ? AppService.Quit(true) : AppService.KeepRunning();
    call.catch((e: unknown) => console.error("answer the quit request", e));
  },
}));

// The Go side pushes every change; nothing here polls.
WailsEvents.On(Events.dataChanged, (event) => {
  const v: DataView = event.data;
  for (const c of loading) c.data = true;
  useData.setState({ profiles: v.profiles ?? [], proxies: v.proxies ?? [] });
});

WailsEvents.On(Events.sessionsChanged, (event) => {
  const v: SessionView = event.data;
  for (const c of loading) c.sessions.add(v.profileId);
  useData.setState((s) => ({ sessions: { ...s.sessions, [v.profileId]: v } }));
});

WailsEvents.On(Events.sessionLog, (event) => {
  const line: LogLine = event.data;
  const id = line.profile;
  if (!id) return;
  useData.setState((s) => ({ logs: { ...s.logs, [id]: appendLog(s.logs[id], line) } }));
});

WailsEvents.On(Events.notice, (event) => {
  const n: Notice = event.data;
  useData.setState((s) => (s.notices.some((x) => x.id === n.id) ? s : { notices: [...s.notices, n] }));
});

WailsEvents.On(Events.quitRequested, (event) => {
  useData.setState({ quitConfirm: event.data.connected });
});
