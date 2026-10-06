import { useEffect, useState } from "react";
import { Body1, Button, Caption1, Card, Subtitle2, Text, makeStyles, tokens } from "@fluentui/react-components";
import { PlugConnected20Regular } from "@fluentui/react-icons";
import type { CancellablePromise } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import { LOCAL_SOURCE, ProxyService, type LocalProbe, type Proxy } from "../../api/backend";
import { useNotify } from "../../components/Feedback";
import { joinHostPort } from "../../lib/address";
import { kindName } from "./names";
import { addOffer, offerName, offerOf, proxyOf, type LocalOffer } from "./localOffers";
import { ProxyDialog } from "./ProxyDialog";

const useStyles = makeStyles({
  card: {
    alignSelf: "center",
    width: "100%",
    maxWidth: "520px",
    boxSizing: "border-box",
    padding: "16px 20px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  heading: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
  },
  icon: {
    fontSize: "20px",
    color: tokens.colorBrandForeground1,
  },
  hint: {
    color: tokens.colorNeutralForeground3,
  },
  row: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) auto",
    alignItems: "center",
    columnGap: "12px",
    paddingTop: "10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke3}`,
  },
  text: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    minWidth: 0,
  },
  sub: {
    color: tokens.colorNeutralForeground3,
    overflowWrap: "anywhere",
  },
});

/**
 * Proxies already running on this computer, each with a button to add it:
 * for a user who has no proxy of their own yet. Shown once a program's port
 * has answered as SOCKS5; nothing at all when none is found. Ports that do
 * not answer are asked until the card goes away.
 */
export function LocalProxies() {
  const styles = useStyles();
  const { t } = useTranslation();
  const notify = useNotify();
  const [offers, setOffers] = useState<LocalOffer[]>([]);
  const [busy, setBusy] = useState(false);
  // A SOCKS5 port that wants an account opens the editor, filled in.
  const [draft, setDraft] = useState<Proxy | null>(null);

  useEffect(() => {
    let live = true;
    const probes: CancellablePromise<LocalProbe>[] = [];
    const found = ProxyService.LocalProxies();
    found.then(
      (candidates) => {
        if (!live) return;
        (candidates ?? []).forEach((c, i) => {
          if (c.source !== LOCAL_SOURCE.program) {
            setOffers((list) => addOffer(list, offerOf(i, c)));
            return;
          }
          const probe = ProxyService.ProbeLocal(c.hosts, c.port);
          probes.push(probe);
          probe.then(
            (r) => live && setOffers((list) => addOffer(list, offerOf(i, c, r))),
            // Refused (the program stopped meanwhile), or cancelled: nothing to offer.
            () => {},
          );
        });
      },
      // Cancelled when the card went away: nothing to say.
      (e: unknown) => live && console.error("look for proxies on this computer", e),
    );
    return () => {
      live = false;
      found.cancel();
      for (const p of probes) p.cancel();
    };
  }, []);

  if (offers.length === 0 && !draft) return null;

  const named = (program: string) => (program ? t("proxies.local.proxyName", { name: program }) : t("proxies.local.proxyNameUnknown"));

  const add = async (offer: LocalOffer) => {
    const proxy = proxyOf(offer, offerName(offer, offers, named));
    if (offer.password) {
      setDraft(proxy);
      return;
    }
    setBusy(true);
    try {
      // The new proxy makes this card go away: the user has one now.
      await ProxyService.Create(proxy);
      notify.success(t("proxies.local.added", { name: proxy.name }));
    } catch (e) {
      notify.error(e, t("proxies.local.addFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      {offers.length > 0 && (
        <Card className={styles.card} role="region" aria-labelledby="local-proxies-title">
          <div className={styles.heading}>
            <PlugConnected20Regular className={styles.icon} />
            <Subtitle2 as="h2" id="local-proxies-title">
              {t("proxies.local.title")}
            </Subtitle2>
          </div>
          <Caption1 className={styles.hint}>{t("proxies.local.body")}</Caption1>
          {offers.map((o) => {
            const sub = [joinHostPort(o.host, o.port, -1), kindName(t, o.kind)];
            if (o.system) sub.push(t("proxies.local.fromSettings"));
            if (o.password) sub.push(t("proxies.local.needsAccount"));
            return (
              <div key={`${o.host}:${o.port}`} className={styles.row}>
                <div className={styles.text}>
                  <Body1>
                    <Text weight="semibold">{o.program || t("proxies.local.unknownProgram")}</Text>
                  </Body1>
                  <Caption1 className={styles.sub}>{sub.join(" · ")}</Caption1>
                </div>
                {/* The page's own button stays the one primary button. */}
                <Button disabled={busy} onClick={() => void add(o)}>
                  {o.password ? t("proxies.local.addWithAccount") : t("proxies.local.add")}
                </Button>
              </div>
            );
          })}
        </Card>
      )}
      {draft && <ProxyDialog view={null} draft={draft} onClose={() => setDraft(null)} />}
    </>
  );
}
