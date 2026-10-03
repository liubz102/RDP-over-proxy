import { useRef, useState } from "react";
import {
  Body1,
  Button,
  Caption1,
  Card,
  Dropdown,
  Menu,
  MenuDivider,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  Option,
  Spinner,
  Subtitle2,
  Text,
  Tooltip,
  makeStyles,
  mergeClasses,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  ArrowImport20Regular,
  DismissCircle20Regular,
  Delete20Regular,
  DesktopArrowRight20Regular,
  DesktopArrowRight24Regular,
  DocumentText20Regular,
  Edit20Regular,
  Key20Regular,
  MoreHorizontal20Regular,
  Pulse20Regular,
  Stop20Regular,
  Window20Regular,
} from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { ProfileService, RDP_PORT, SessionService, type ProfileView } from "../../api/backend";
import { ConfirmDialog, useNotify } from "../../components/Feedback";
import { EmptyState, Page } from "../../components/Page";
import { joinHostPort } from "../../lib/address";
import { errorText } from "../../lib/messages";
import { useData } from "../../stores/data";
import { proxyName } from "../proxies/names";
import { CheckDialog } from "./CheckDialog";
import { PasswordDialog } from "./PasswordDialog";
import { ProfileDialog } from "./ProfileDialog";
import { importRdp, type Imported } from "./rdpImport";
import { SessionLog } from "./SessionLog";
import { sessionStatus, type Tone } from "./status";

const useStyles = makeStyles({
  group: {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
  },
  groupTitle: {
    color: tokens.colorNeutralForeground2,
    paddingLeft: "4px",
  },
  list: {
    padding: "0",
    gap: "0",
  },
  row: {
    display: "grid",
    gridTemplateColumns: "auto minmax(0, 1fr) auto",
    alignItems: "center",
    columnGap: "14px",
    padding: "12px 12px 12px 16px",
    ":not(:last-child)": {
      borderBottom: `1px solid ${tokens.colorNeutralStroke3}`,
    },
  },
  dot: {
    width: "10px",
    height: "10px",
    borderRadius: "50%",
    flexShrink: 0,
  },
  idle: { backgroundColor: tokens.colorNeutralStroke1 },
  busy: { backgroundColor: tokens.colorBrandBackground },
  ok: { backgroundColor: tokens.colorPaletteGreenBackground3 },
  warn: { backgroundColor: tokens.colorPaletteYellowBackground3 },
  error: { backgroundColor: tokens.colorPaletteRedBackground3 },
  text: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    minWidth: 0,
  },
  name: {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  sub: {
    color: tokens.colorNeutralForeground3,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  missing: {
    color: tokens.colorStatusDangerForeground1,
  },
  status: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
    minWidth: 0,
  },
  statusText: {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  statusOk: { color: tokens.colorPaletteGreenForeground1 },
  statusWarn: { color: tokens.colorStatusWarningForeground1 },
  statusError: { color: tokens.colorStatusDangerForeground1 },
  statusLink: {
    cursor: "pointer",
    ":hover": { textDecoration: "underline" },
  },
  buttons: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
  },
  proxy: {
    // Fluent's dropdowns are at least 250px wide.
    minWidth: "0",
    width: "160px",
  },
  proxyValue: {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    minWidth: 0,
  },
  proxyList: {
    maxWidth: "320px",
  },
  emptyActions: {
    display: "flex",
    gap: "8px",
    justifyContent: "center",
  },
});

type Dialog =
  | { kind: "edit"; view: ProfileView | null; imported?: Imported }
  | { kind: "password"; view: ProfileView }
  | { kind: "check"; view: ProfileView }
  | { kind: "log"; view: ProfileView }
  | { kind: "delete"; view: ProfileView };

/** The profiles in their groups, in the order the Go side lists them (by group, then name). */
function byGroup(profiles: ProfileView[]): [string, ProfileView[]][] {
  const groups = new Map<string, ProfileView[]>();
  for (const p of profiles) {
    const list = groups.get(p.profile.group);
    if (list) list.push(p);
    else groups.set(p.profile.group, [p]);
  }
  return [...groups];
}

export function ConnectionsPage() {
  const styles = useStyles();
  const { t } = useTranslation();
  const notify = useNotify();
  const profiles = useData((s) => s.profiles);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [busy, setBusy] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const close = () => setDialog(null);

  // An .rdp file fills in the editor of a new profile; saving stores it.
  // A dialog the user opened meanwhile stays: the editor reads what it
  // starts from only when it opens.
  const importFile = async (file: File) => {
    try {
      const imported = await importRdp(file);
      setDialog((d) => d ?? { kind: "edit", view: null, imported });
    } catch (e) {
      notify.error(e, t("connections.import.failed", { file: file.name }));
    }
  };

  const connect = async (v: ProfileView, password: string, remember?: boolean) => {
    close();
    try {
      // The prompt can change whether the password is remembered.
      if (remember !== undefined && remember !== v.profile.rememberPassword) {
        await ProfileService.Update({ ...v.profile, rememberPassword: remember }, "");
      }
      await SessionService.Connect(v.profile.id, password);
    } catch (e) {
      notify.error(e, t("connections.connectFailed", { name: v.profile.name }));
    }
  };

  const startConnect = (v: ProfileView) => {
    // Without a user name a password cannot be handed to Remote Desktop; it asks for both itself.
    if (!v.passwordSaved && !v.passwordByMstsc && v.profile.username !== "") {
      setDialog({ kind: "password", view: v });
    } else {
      void connect(v, "");
    }
  };

  const disconnect = async (v: ProfileView, force: boolean) => {
    try {
      await SessionService.Disconnect(v.profile.id, force);
    } catch (e) {
      notify.error(e);
    }
  };

  const focus = async (v: ProfileView) => {
    try {
      await SessionService.Focus(v.profile.id);
    } catch (e) {
      notify.error(e);
    }
  };

  const forget = async (v: ProfileView) => {
    try {
      await ProfileService.ForgetPassword(v.profile.id);
      notify.success(t("connections.form.forgotten"));
    } catch (e) {
      notify.error(e);
    }
  };

  const remove = async (v: ProfileView) => {
    setBusy(true);
    try {
      await ProfileService.Delete(v.profile.id);
      close();
    } catch (e) {
      close();
      notify.error(e, t("connections.deleteFailed", { name: v.profile.name }));
    } finally {
      setBusy(false);
    }
  };

  const add = (
    <Button appearance="primary" icon={<Add20Regular />} onClick={() => setDialog({ kind: "edit", view: null })}>
      {t("connections.add")}
    </Button>
  );
  const importButton = (
    <Button icon={<ArrowImport20Regular />} onClick={() => fileInput.current?.click()}>
      {t("connections.import.button")}
    </Button>
  );

  const groups = byGroup(profiles);
  const showGroupTitles = groups.some(([g]) => g !== "");

  return (
    <Page
      title={t("connections.title")}
      subtitle={profiles.length > 0 ? t("connections.subtitle") : undefined}
      actions={
        profiles.length > 0 && (
          <>
            {importButton}
            {add}
          </>
        )
      }
    >
      <input
        ref={fileInput}
        type="file"
        accept=".rdp"
        hidden
        onChange={(e) => {
          const file = e.target.files?.[0];
          // Cleared, so choosing the same file again reads it again.
          e.target.value = "";
          if (file) void importFile(file);
        }}
      />
      {profiles.length === 0 && (
        <EmptyState
          icon={<DesktopArrowRight24Regular />}
          title={t("connections.emptyTitle")}
          body={t("connections.emptyBody")}
          action={
            <div className={styles.emptyActions}>
              {add}
              {importButton}
            </div>
          }
        />
      )}
      {groups.map(([group, list]) => (
        <section key={group} className={styles.group}>
          {showGroupTitles && (
            <Subtitle2 as="h2" className={styles.groupTitle}>
              {group || t("connections.ungrouped")}
            </Subtitle2>
          )}
          <Card className={styles.list}>
            {list.map((v) => (
              <Row
                key={v.profile.id}
                view={v}
                onConnect={() => startConnect(v)}
                onDisconnect={(force) => void disconnect(v, force)}
                onFocus={() => void focus(v)}
                onForget={() => void forget(v)}
                onDialog={(kind) => setDialog({ kind, view: v } as Dialog)}
              />
            ))}
          </Card>
        </section>
      ))}

      {dialog?.kind === "edit" && <ProfileDialog view={dialog.view} imported={dialog.imported} onClose={close} />}
      {dialog?.kind === "password" && (
        <PasswordDialog
          view={dialog.view}
          onClose={close}
          onConnect={(password, remember) => void connect(dialog.view, password, remember)}
        />
      )}
      {dialog?.kind === "check" && <CheckDialog view={dialog.view} onClose={close} />}
      {dialog?.kind === "log" && <SessionLog view={dialog.view} onClose={close} />}
      <ConfirmDialog
        open={dialog?.kind === "delete"}
        title={t("connections.deleteTitle")}
        confirm={t("common.delete")}
        danger
        busy={busy}
        onClose={close}
        onConfirm={() => dialog?.kind === "delete" && void remove(dialog.view)}
      >
        {dialog?.kind === "delete" && t("connections.deleteBody", { name: dialog.view.profile.name })}
      </ConfirmDialog>
    </Page>
  );
}

function Row({
  view,
  onConnect,
  onDisconnect,
  onFocus,
  onForget,
  onDialog,
}: {
  view: ProfileView;
  onConnect: () => void;
  onDisconnect: (force: boolean) => void;
  onFocus: () => void;
  onForget: () => void;
  onDialog: (kind: "edit" | "check" | "log" | "delete") => void;
}) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const p = view.profile;
  const session = useData((s) => s.sessions[p.id]);
  const proxy = useData((s) => s.proxies.find((x) => x.proxy.id === p.proxyId));
  const status = sessionStatus(session);
  const active = status.actions !== "connect";

  const toneClass: Record<Tone, string> = {
    idle: styles.idle,
    busy: styles.busy,
    ok: styles.ok,
    warn: styles.warn,
    error: styles.error,
  };
  const textClass =
    status.tone === "ok" ? styles.statusOk : status.tone === "warn" ? styles.statusWarn : status.tone === "error" ? styles.statusError : undefined;

  let statusLine: string | null = null;
  if (status.label) {
    const args = { ...status.args };
    if (args.step) args.step = t(`steps.${args.step}`);
    statusLine = t(status.label, args);
    if (status.error) statusLine += t("common.colon") + errorText(i18n, status.error);
  }

  return (
    <div className={styles.row}>
      <span className={mergeClasses(styles.dot, toneClass[status.tone])} />
      <div className={styles.text}>
        <Body1 className={styles.name}>
          <Text weight="semibold">{p.name}</Text>
        </Body1>
        <Caption1 className={styles.sub}>
          {joinHostPort(p.target.host, p.target.port, RDP_PORT)}
          {/* While connected the proxy is only said; otherwise the dropdown chooses it. */}
          {active && (
            <>
              {" · "}
              {view.proxyMissing || !proxy ? (
                <span className={styles.missing}>{t("connections.proxyMissing")}</span>
              ) : (
                t("connections.via", { proxy: proxyName(t, proxy.proxy) })
              )}
            </>
          )}
          {p.username && ` · ${p.username}`}
        </Caption1>
        {statusLine && (
          <div className={styles.status}>
            {status.tone === "busy" && <Spinner size="extra-tiny" />}
            <Tooltip content={t("connections.showLog")} relationship="description">
              <Caption1
                className={mergeClasses(styles.statusText, styles.statusLink, textClass)}
                onClick={() => onDialog("log")}
              >
                {statusLine}
              </Caption1>
            </Tooltip>
          </div>
        )}
      </div>
      <div className={styles.buttons}>
        {!active && <ProxyPicker view={view} />}
        {status.actions === "connect" && (
          <Button appearance="primary" icon={<DesktopArrowRight20Regular />} onClick={onConnect}>
            {t("connections.connect")}
          </Button>
        )}
        {status.actions === "cancel" && (
          <Button icon={<Stop20Regular />} onClick={() => onDisconnect(false)}>
            {t("common.cancel")}
          </Button>
        )}
        {status.actions === "running" && (
          <>
            <Button icon={<Window20Regular />} onClick={onFocus}>
              {t("connections.focus")}
            </Button>
            <Button onClick={() => onDisconnect(false)}>{t("connections.disconnect")}</Button>
          </>
        )}
        {status.actions === "ending" && (
          <Button icon={<Stop20Regular />} onClick={() => onDisconnect(true)}>
            {t("connections.kill")}
          </Button>
        )}
        <Menu positioning="below-end">
          <MenuTrigger disableButtonEnhancement>
            <Button appearance="subtle" icon={<MoreHorizontal20Regular />} aria-label={t("common.more")} />
          </MenuTrigger>
          <MenuPopover>
            <MenuList>
              <MenuItem icon={<Edit20Regular />} onClick={() => onDialog("edit")}>
                {t("common.edit")}
              </MenuItem>
              <MenuItem icon={<Pulse20Regular />} disabled={active} onClick={() => onDialog("check")}>
                {t("connections.checkRoute")}
              </MenuItem>
              <MenuItem icon={<DocumentText20Regular />} onClick={() => onDialog("log")}>
                {t("connections.showLog")}
              </MenuItem>
              {(view.passwordSaved || view.passwordByMstsc) && (
                <MenuItem icon={<Key20Regular />} disabled={active} onClick={onForget}>
                  {t("connections.form.forget")}
                </MenuItem>
              )}
              {status.actions === "running" && (
                <MenuItem icon={<DismissCircle20Regular />} onClick={() => onDisconnect(true)}>
                  {t("connections.kill")}
                </MenuItem>
              )}
              <MenuDivider />
              <MenuItem icon={<Delete20Regular />} disabled={active} onClick={() => onDialog("delete")}>
                {t("common.delete")}
              </MenuItem>
            </MenuList>
          </MenuPopover>
        </Menu>
      </div>
    </div>
  );
}

/** Chooses the proxy of a profile that is not connected; a connected one keeps its own. */
function ProxyPicker({ view }: { view: ProfileView }) {
  const styles = useStyles();
  const { t } = useTranslation();
  const notify = useNotify();
  const proxies = useData((s) => s.proxies);
  const p = view.profile;
  // The choice shows while it is saved; then the data the Go side sends does.
  const [saving, setSaving] = useState<string | null>(null);
  const latest = useRef(0);
  const shown = proxies.find((x) => x.proxy.id === (saving ?? p.proxyId));
  const text = shown ? proxyName(t, shown.proxy) : t("connections.proxyMissing");

  const choose = async (proxyId: string) => {
    if (proxyId === (saving ?? p.proxyId)) return;
    const call = ++latest.current;
    setSaving(proxyId);
    try {
      await ProfileService.SetProxy(p.id, proxyId);
    } catch (e) {
      notify.error(e, t("connections.setProxyFailed", { name: p.name }));
    } finally {
      if (latest.current === call) setSaving(null);
    }
  };

  return (
    <Dropdown
      className={styles.proxy}
      aria-label={t("connections.proxy")}
      value={text}
      selectedOptions={shown ? [shown.proxy.id] : []}
      // Fluent puts the bare text in the button, where a long name would not end in "…".
      button={{ children: <span className={mergeClasses(styles.proxyValue, !shown && styles.missing)}>{text}</span> }}
      // The list is as wide as its names need (from the button's width up to
      // proxyList's), growing to the left: the button sits near the right edge.
      // Fluent would size its width to the room left, over proxyList's.
      positioning={{ matchTargetSize: undefined, align: "end", autoSize: "height" }}
      listbox={{ className: styles.proxyList }}
      onOptionSelect={(_, d) => d.optionValue && void choose(d.optionValue)}
    >
      {proxies.map((x) => (
        <Option key={x.proxy.id} value={x.proxy.id} text={proxyName(t, x.proxy)}>
          {proxyName(t, x.proxy)}
        </Option>
      ))}
    </Dropdown>
  );
}
