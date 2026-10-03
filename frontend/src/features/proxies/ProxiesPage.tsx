import { useEffect, useRef, useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Caption1,
  Card,
  Menu,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  Spinner,
  Text,
  Tooltip,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  Delete20Regular,
  Edit20Regular,
  Globe24Regular,
  Link20Regular,
  MoreHorizontal20Regular,
  TopSpeed20Regular,
} from "@fluentui/react-icons";
import { Clipboard, type CancellablePromise } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import { errorOf, ProxyService, type ErrorView, type LatencyResult, type ProxyView } from "../../api/backend";
import { ConfirmDialog, ErrorBar, useNotify } from "../../components/Feedback";
import { EmptyState, Page } from "../../components/Page";
import { joinHostPort } from "../../lib/address";
import { errorText } from "../../lib/messages";
import { useData } from "../../stores/data";
import { kindName, proxyName, transportName } from "./names";
import { ProxyDialog } from "./ProxyDialog";

const useStyles = makeStyles({
  list: {
    padding: "0",
    gap: "0",
  },
  row: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) auto",
    alignItems: "center",
    columnGap: "14px",
    padding: "12px 12px 12px 16px",
    ":not(:last-child)": {
      borderBottom: `1px solid ${tokens.colorNeutralStroke3}`,
    },
  },
  text: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    minWidth: 0,
  },
  title: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
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
  buttons: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
  },
  latency: {
    minWidth: "72px",
    textAlign: "right",
  },
  good: { color: tokens.colorPaletteGreenForeground1 },
  bad: {
    color: tokens.colorStatusDangerForeground1,
    // Caption1 is a span; only a block of some kind keeps to its width.
    display: "inline-block",
    verticalAlign: "middle",
    maxWidth: "220px",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  menuPlaceholder: {
    width: "32px",
  },
});

type Dialog = { kind: "edit"; view: ProxyView | null } | { kind: "delete"; view: ProxyView };

export function ProxiesPage() {
  const styles = useStyles();
  const { t } = useTranslation();
  const proxies = useData((s) => s.proxies);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [deleteError, setDeleteError] = useState<ErrorView | null>(null);
  const [busy, setBusy] = useState(false);
  const close = () => {
    setDialog(null);
    setDeleteError(null);
  };

  const remove = async (v: ProxyView) => {
    setBusy(true);
    try {
      await ProxyService.Delete(v.proxy.id);
      close();
    } catch (e) {
      // A proxy that connections use stays; the error names them.
      setDeleteError(errorOf(e));
    } finally {
      setBusy(false);
    }
  };

  const own = proxies.filter((p) => !p.builtIn);
  const add = (
    <Button appearance="primary" icon={<Add20Regular />} onClick={() => setDialog({ kind: "edit", view: null })}>
      {t("proxies.add")}
    </Button>
  );

  return (
    <Page title={t("proxies.title")} subtitle={t("proxies.subtitle")} actions={own.length > 0 ? add : undefined}>
      <Card className={styles.list}>
        {proxies.map((v) => (
          <Row key={v.proxy.id} view={v} onDialog={(kind) => setDialog({ kind, view: v } as Dialog)} />
        ))}
      </Card>
      {own.length === 0 && (
        <EmptyState icon={<Globe24Regular />} title={t("proxies.emptyTitle")} body={t("proxies.emptyBody")} action={add} />
      )}

      {dialog?.kind === "edit" && <ProxyDialog view={dialog.view} onClose={close} />}
      <ConfirmDialog
        open={dialog?.kind === "delete"}
        title={t("proxies.deleteTitle")}
        confirm={t("common.delete")}
        danger
        busy={busy || deleteError !== null}
        onClose={close}
        onConfirm={() => dialog?.kind === "delete" && void remove(dialog.view)}
      >
        {dialog?.kind === "delete" && (deleteError ? <ErrorBar error={deleteError} /> : t("proxies.deleteBody", { name: dialog.view.proxy.name }))}
      </ConfirmDialog>
    </Page>
  );
}

type Latency =
  | { kind: "idle" }
  | { kind: "running"; call: CancellablePromise<LatencyResult> }
  | { kind: "done"; ms: number }
  | { kind: "failed"; error: ErrorView };

/** Puts text on the clipboard: the page's own way first, Wails' when the page may not. */
async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    await Clipboard.SetText(text);
  }
}

function Row({ view, onDialog }: { view: ProxyView; onDialog: (kind: "edit" | "delete") => void }) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const notify = useNotify();
  const p = view.proxy;
  const [latency, setLatency] = useState<Latency>({ kind: "idle" });
  const running = useRef<CancellablePromise<LatencyResult> | null>(null);

  // Leaving the page stops a test still running.
  useEffect(
    () => () => {
      running.current?.cancel();
    },
    [],
  );

  const test = () => {
    if (latency.kind === "running") {
      running.current = null;
      latency.call.cancel();
      setLatency({ kind: "idle" });
      return;
    }
    const call = ProxyService.Latency(p.id);
    running.current = call;
    setLatency({ kind: "running", call });
    call.then(
      (r) => {
        if (running.current !== call) return;
        running.current = null;
        setLatency({ kind: "done", ms: r.ms });
      },
      (e: unknown) => {
        if (running.current !== call) return; // cancelled
        running.current = null;
        setLatency({ kind: "failed", error: errorOf(e) });
      },
    );
  };

  const copyLink = async () => {
    try {
      await copy(await ProxyService.ShareLink(p.id));
      notify.success(t("proxies.linkCopied"));
    } catch (e) {
      notify.error(e, t("proxies.linkCopyFailed"));
    }
  };

  const sub: string[] = [];
  if (view.builtIn) sub.push(t("proxies.directHint"));
  else {
    if (p.server) sub.push(joinHostPort(p.server, p.port, -1));
    if (p.username) sub.push(t("proxies.account", { user: p.username }));
    const transport = transportName(t, view.network, view.security);
    if (transport) sub.push(transport);
  }
  sub.push(view.usedBy > 0 ? t("proxies.usedBy", { count: view.usedBy }) : t("proxies.unused"));

  return (
    <div className={styles.row}>
      <div className={styles.text}>
        <div className={styles.title}>
          <Body1 className={styles.name}>
            <Text weight="semibold">{proxyName(t, p)}</Text>
          </Body1>
          {!view.builtIn && (
            <Badge appearance="tint" color="informative" size="small">
              {kindName(t, p.kind)}
            </Badge>
          )}
          {view.secretsLost && (
            <Tooltip content={t("proxies.secretsLostHint")} relationship="description">
              <Badge appearance="tint" color="warning" size="small">
                {t("proxies.secretsLost")}
              </Badge>
            </Tooltip>
          )}
        </div>
        <Caption1 className={styles.sub}>{sub.join(" · ")}</Caption1>
      </div>
      <div className={styles.buttons}>
        <div className={styles.latency}>
          {latency.kind === "running" && <Spinner size="extra-tiny" />}
          {latency.kind === "done" && <Caption1 className={styles.good}>{t("proxies.latencyMs", { ms: latency.ms })}</Caption1>}
          {latency.kind === "failed" && (
            <Tooltip content={latency.error.message || errorText(i18n, latency.error)} relationship="description">
              <Caption1 className={styles.bad}>{errorText(i18n, latency.error)}</Caption1>
            </Tooltip>
          )}
        </div>
        <Tooltip content={t("proxies.latencyHint")} relationship="description">
          <Button icon={<TopSpeed20Regular />} onClick={test}>
            {latency.kind === "running" ? t("common.cancel") : t("proxies.latency")}
          </Button>
        </Tooltip>
        {view.builtIn ? (
          <span className={styles.menuPlaceholder} />
        ) : (
          <Menu positioning="below-end">
            <MenuTrigger disableButtonEnhancement>
              <Button appearance="subtle" icon={<MoreHorizontal20Regular />} aria-label={t("common.more")} />
            </MenuTrigger>
            <MenuPopover>
              <MenuList>
                <MenuItem icon={<Edit20Regular />} onClick={() => onDialog("edit")}>
                  {t("common.edit")}
                </MenuItem>
                {p.kind !== "xray" && (
                  <MenuItem icon={<Link20Regular />} onClick={() => void copyLink()}>
                    {t("proxies.copyLink")}
                  </MenuItem>
                )}
                <MenuItem icon={<Delete20Regular />} onClick={() => onDialog("delete")}>
                  {t("common.delete")}
                </MenuItem>
              </MenuList>
            </MenuPopover>
          </Menu>
        )}
      </div>
    </div>
  );
}
