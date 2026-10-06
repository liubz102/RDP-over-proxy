import { memo, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  Body1,
  Button,
  Caption1,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  Dropdown,
  Option,
  OverlayDrawer,
  SearchBox,
  makeStyles,
  mergeClasses,
  tokens,
} from "@fluentui/react-components";
import { Dismiss24Regular } from "@fluentui/react-icons";
import { Events as WailsEvents } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import { AppService, Events, type LogLine } from "../../api/backend";
import { APP_LOG_LINES, argsText, atLeast, insertLine, mergeLines } from "../../lib/appLog";
import { logDetails, logText } from "../../lib/messages";
import { useData } from "../../stores/data";
import { useSettings } from "../../stores/settings";

const useStyles = makeStyles({
  intro: {
    color: tokens.colorNeutralForeground3,
  },
  tools: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: "8px",
    padding: "4px 0 12px",
  },
  level: {
    minWidth: "0",
    width: "180px",
  },
  search: {
    flexGrow: 1,
    maxWidth: "320px",
  },
  // One grid for every line, so the times and levels line up.
  lines: {
    display: "grid",
    gridTemplateColumns: "max-content max-content minmax(0, 1fr)",
    columnGap: "10px",
    rowGap: "6px",
    alignItems: "baseline",
    paddingBottom: "8px",
  },
  empty: {
    gridColumn: "1 / -1",
    color: tokens.colorNeutralForeground3,
  },
  mono: {
    fontFamily: tokens.fontFamilyMonospace,
    color: tokens.colorNeutralForeground3,
    whiteSpace: "nowrap",
  },
  levelName: {
    color: tokens.colorNeutralForeground3,
  },
  warn: {
    color: tokens.colorStatusWarningForeground1,
  },
  error: {
    color: tokens.colorStatusDangerForeground1,
  },
  text: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    minWidth: 0,
    wordBreak: "break-word",
  },
  source: {
    color: tokens.colorNeutralForeground3,
  },
  details: {
    color: tokens.colorNeutralForeground3,
    fontFamily: tokens.fontFamilyMonospace,
    fontSize: tokens.fontSizeBase200,
  },
});

// Made once: formatting a thousand dates anew on every line that comes in
// would show.
const clock = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
const day = new Intl.DateTimeFormat(undefined, { month: "2-digit", day: "2-digit" });

/** The time of a line; with the date when it is not from today. */
function when(time: string, today: string): string {
  const d = new Date(time);
  if (Number.isNaN(d.getTime())) return "";
  return d.toDateString() === today ? clock.format(d) : `${day.format(d)} ${clock.format(d)}`;
}

/** The levels the filter offers: every line, warnings and errors, errors. */
const filters = ["", "warn", "error"] as const;

/** What one line shows, worked out once. */
interface LineView {
  key: string;
  when: string;
  level: string;
  tone: "error" | "warn" | "";
  source: string;
  colon: string;
  text: string;
  details: string;
  /** All of the text in lower case, for the search. */
  search: string;
}

/** One line: three cells of the list's grid. Lines already shown are not drawn again. */
const LogRow = memo(function LogRow({ view }: { view: LineView }) {
  const styles = useStyles();
  const tone = view.tone === "error" ? styles.error : view.tone === "warn" ? styles.warn : styles.levelName;
  return (
    <>
      <Caption1 className={styles.mono}>{view.when}</Caption1>
      <Caption1 className={tone}>{view.level}</Caption1>
      <div className={styles.text}>
        <Body1>
          <span className={styles.source}>{view.source}</span>
          {view.colon}
          <span className={mergeClasses(view.tone === "error" && styles.error)}>{view.text}</span>
        </Body1>
        {view.details && <Caption1 className={styles.details}>{view.details}</Caption1>}
      </div>
    </>
  );
});

/**
 * The app's own recent log, kept up to date while open: what the app, Xray,
 * the sessions and the window's runtime said, unmasked. The log file is
 * the masked copy, for bug reports.
 */
export function AppLog({ onClose }: { onClose: () => void }) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const logLevel = useSettings((s) => s.settings?.logLevel ?? "info");
  const profiles = useData((s) => s.profiles);
  const [lines, setLines] = useState<LogLine[]>([]);
  const [level, setLevel] = useState<(typeof filters)[number]>("");
  const [query, setQuery] = useState("");
  const body = useRef<HTMLDivElement>(null);
  // Whether the view is at the newest line, so new lines keep it there.
  const atEnd = useRef(true);

  // Listen first, then read: lines kept in between come as events, and
  // Seq merges the two.
  useEffect(() => {
    let live = true;
    let pending: LogLine[] = [];
    let frame = 0;
    // Lines that come together are shown together, once per frame of the
    // display: a burst of Xray's lines is drawn once.
    const flush = () => {
      frame = 0;
      const batch = pending;
      pending = [];
      setLines((ls) => batch.reduce(insertLine, ls));
    };
    const off = WailsEvents.On(Events.appLog, (event) => {
      pending.push(event.data as LogLine);
      // While the window is hidden no frame comes; only the newest lines
      // would be shown anyway.
      if (pending.length > APP_LOG_LINES) pending = pending.slice(-APP_LOG_LINES);
      if (frame === 0) frame = requestAnimationFrame(flush);
    });
    const read = AppService.Log();
    read.then(
      (r) => live && setLines((ls) => mergeLines(r ?? [], ls)),
      // Cancelled when the drawer closed: nothing to say.
      (e: unknown) => live && console.error("read the app's log", e),
    );
    return () => {
      live = false;
      off();
      read.cancel();
      cancelAnimationFrame(frame);
    };
  }, []);

  // Each line's view is kept until what it shows could have changed: the
  // language, the connections' names, or the day (times of other days show
  // their date).
  const today = new Date().toDateString();
  const views = useMemo(() => new WeakMap<LogLine, LineView>(), [i18n.language, profiles, today]);
  const viewOf = (l: LogLine): LineView => {
    let v = views.get(l);
    if (!v) {
      const session = l.source === "session";
      const name = session ? (profiles.find((p) => p.profile.id === l.profile)?.profile.name ?? l.profile ?? "") : "";
      const source = session ? t("appLog.session", { name }) : t(`appLog.sources.${l.source}`, { defaultValue: l.source });
      // Session lines are keys the UI translates; the others are English text.
      const text = session ? logText(i18n, l) : l.msg;
      const details = session ? logDetails(i18n, l) : argsText(l.args);
      v = {
        key: String(l.seq ?? `${l.time}-${l.msg}`),
        when: when(l.time, today),
        level: t(`appLog.levels.${l.level}`, { defaultValue: l.level }),
        tone: l.level === "error" ? "error" : l.level === "warn" ? "warn" : "",
        source,
        colon: t("common.colon"),
        text,
        details,
        search: `${source} ${text} ${details}`.toLowerCase(),
      };
      views.set(l, v);
    }
    return v;
  };
  const q = query.trim().toLowerCase();
  const shown = lines
    .filter((l) => atLeast(l, level))
    .map(viewOf)
    .filter((v) => q === "" || v.search.includes(q));

  useLayoutEffect(() => {
    const el = body.current;
    if (el && atEnd.current) el.scrollTop = el.scrollHeight;
  }, [shown.length, lines]);

  const filterName = (f: string) => t(`appLog.filters.${f || "all"}`);

  return (
    <OverlayDrawer open position="end" size="large" onOpenChange={(_, d) => !d.open && onClose()}>
      <DrawerHeader>
        <DrawerHeaderTitle
          action={<Button appearance="subtle" aria-label={t("common.close")} icon={<Dismiss24Regular />} onClick={onClose} />}
        >
          {t("appLog.title")}
        </DrawerHeaderTitle>
        <Caption1 className={styles.intro}>{t("appLog.intro", { level: t(`settings.logLevels.${logLevel}`) })}</Caption1>
        <div className={styles.tools}>
          <Dropdown
            className={styles.level}
            aria-label={t("appLog.filter")}
            value={filterName(level)}
            selectedOptions={[level || "all"]}
            onOptionSelect={(_, d) => setLevel(d.optionValue === "all" ? "" : ((d.optionValue ?? "") as typeof level))}
          >
            {filters.map((f) => (
              <Option key={f || "all"} value={f || "all"}>
                {filterName(f)}
              </Option>
            ))}
          </Dropdown>
          <SearchBox
            className={styles.search}
            aria-label={t("appLog.search")}
            placeholder={t("appLog.search")}
            value={query}
            onChange={(_, d) => setQuery(d.value)}
          />
        </div>
      </DrawerHeader>
      <DrawerBody
        ref={body}
        onScroll={() => {
          const el = body.current;
          // Within a line's height of the end counts as at the end.
          if (el) atEnd.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
      >
        <div className={styles.lines} role="log" aria-label={t("appLog.title")}>
          {shown.length === 0 && (
            <Body1 className={styles.empty}>{lines.length === 0 ? t("appLog.empty") : t("appLog.noMatch")}</Body1>
          )}
          {shown.map((v) => (
            <LogRow key={v.key} view={v} />
          ))}
        </div>
      </DrawerBody>
    </OverlayDrawer>
  );
}
