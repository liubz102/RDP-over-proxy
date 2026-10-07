import { useEffect, useState } from "react";
import {
  Body1,
  Button,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  OverlayDrawer,
  Spinner,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Dismiss24Regular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { loadThirdParty } from "../../lib/thirdParty";

const useStyles = makeStyles({
  text: {
    margin: 0,
    paddingBottom: "16px",
    fontFamily: tokens.fontFamilyMonospace,
    fontSize: tokens.fontSizeBase200,
    lineHeight: tokens.lineHeightBase200,
    whiteSpace: "pre-wrap",
    overflowWrap: "anywhere",
  },
  missing: {
    color: tokens.colorNeutralForeground3,
  },
});

/** The licenses and notices of the third-party software in this build. */
export function ThirdPartyNotices({ onClose }: { onClose: () => void }) {
  const styles = useStyles();
  const { t } = useTranslation();
  // undefined while loading, null when the build has none.
  const [text, setText] = useState<string | null>();
  useEffect(() => {
    let open = true;
    loadThirdParty().then(
      (v) => open && setText(v ?? null),
      () => open && setText(null),
    );
    return () => {
      open = false;
    };
  }, []);

  return (
    <OverlayDrawer open position="end" size="large" onOpenChange={(_, d) => !d.open && onClose()}>
      <DrawerHeader>
        <DrawerHeaderTitle
          action={<Button appearance="subtle" aria-label={t("common.close")} icon={<Dismiss24Regular />} onClick={onClose} />}
        >
          {t("settings.thirdPartyTitle")}
        </DrawerHeaderTitle>
      </DrawerHeader>
      <DrawerBody>
        {text === undefined && <Spinner size="small" label={t("common.loading")} />}
        {text === null && <Body1 className={styles.missing}>{t("settings.thirdPartyMissing")}</Body1>}
        {/* Focusable, so the keyboard can scroll it. */}
        {text && (
          <pre className={styles.text} tabIndex={0} aria-label={t("settings.thirdPartyTitle")}>
            {text}
          </pre>
        )}
      </DrawerBody>
    </OverlayDrawer>
  );
}
