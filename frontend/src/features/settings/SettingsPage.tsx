import { Fragment, useEffect, useState, type ReactNode } from "react";
import {
  Body1,
  Button,
  Caption1,
  Card,
  Dropdown,
  Field,
  Input,
  Link,
  MessageBar,
  MessageBarBody,
  Option,
  Radio,
  RadioGroup,
  Subtitle2,
  Switch,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { FolderOpen20Regular } from "@fluentui/react-icons";
import { Browser } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import { AppService, type FolderName } from "../../api/backend";
import { useNotify } from "../../components/Feedback";
import { Page } from "../../components/Page";
import { isLanguage, languages } from "../../i18n";
import { useSettings } from "../../stores/settings";

const useStyles = makeStyles({
  card: {
    padding: "16px 20px 20px",
    display: "flex",
    flexDirection: "column",
    gap: "16px",
  },
  hint: {
    color: tokens.colorNeutralForeground3,
  },
  dropdown: {
    minWidth: "240px",
  },
  port: {
    width: "120px",
  },
  url: {
    maxWidth: "460px",
  },
  about: {
    display: "grid",
    gridTemplateColumns: "max-content 1fr",
    columnGap: "24px",
    rowGap: "8px",
    alignItems: "baseline",
  },
  label: {
    color: tokens.colorNeutralForeground3,
  },
  folders: {
    display: "grid",
    gridTemplateColumns: "max-content minmax(0, 1fr) max-content",
    columnGap: "16px",
    rowGap: "8px",
    alignItems: "center",
  },
  path: {
    overflowWrap: "anywhere",
  },
  files: {
    gridColumn: "2 / 4",
    display: "grid",
    gridTemplateColumns: "max-content 1fr",
    columnGap: "16px",
    rowGap: "2px",
  },
  file: {
    fontFamily: tokens.fontFamilyMonospace,
    color: tokens.colorNeutralForeground2,
  },
});

/** What the data folder holds: the name, and the key of what it is. */
const dataFiles: [string, string][] = [
  ["settings.json", "settings"],
  ["proxies\\", "proxies"],
  ["profiles\\", "profiles"],
  ["WebView2\\", "webview2"],
];

/**
 * A text setting: edited freely, saved when the field loses focus or Enter is
 * pressed, so half-typed values are never stored.
 */
function TextSetting({
  value,
  className,
  check,
  onSave,
}: {
  value: string;
  className?: string;
  /** Returns a problem to show instead of saving. */
  check?: (text: string) => string | undefined;
  onSave: (text: string) => void;
}) {
  const [text, setText] = useState(value);
  useEffect(() => setText(value), [value]);
  const problem = check?.(text);
  const commit = () => {
    if (text !== value && !problem) onSave(text);
  };
  return (
    <>
      <Input
        className={className}
        value={text}
        onChange={(_, d) => setText(d.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === "Enter" && commit()}
      />
      {problem && (
        <Caption1 role="alert" style={{ color: tokens.colorStatusDangerForeground1 }}>
          {problem}
        </Caption1>
      )}
    </>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const styles = useStyles();
  return (
    <Card className={styles.card}>
      <Subtitle2 as="h2">{title}</Subtitle2>
      {children}
    </Card>
  );
}

export function SettingsPage() {
  const styles = useStyles();
  const { t } = useTranslation();
  const settings = useSettings((s) => s.settings);
  const appInfo = useSettings((s) => s.appInfo);
  const folders = useSettings((s) => s.folders);
  const save = useSettings((s) => s.save);
  const saveError = useSettings((s) => s.saveError);
  const notify = useNotify();
  if (!settings) return null;

  const openFolder = async (name: FolderName) => {
    try {
      await AppService.OpenFolder(name);
    } catch (e) {
      notify.error(e, t("settings.openFolderFailed"));
    }
  };
  const openButton = (name: FolderName) => (
    <Button icon={<FolderOpen20Regular />} onClick={() => void openFolder(name)}>
      {t("settings.openFolder")}
    </Button>
  );

  return (
    <Page title={t("settings.title")} subtitle={t("settings.autosave")}>
      {saveError && (
        <MessageBar intent="error">
          <MessageBarBody>{t("settings.saveFailed", { message: saveError })}</MessageBarBody>
        </MessageBar>
      )}

      <Section title={t("settings.appearance")}>
        <Field label={t("settings.language")}>
          <Dropdown
            className={styles.dropdown}
            value={t(`languageName.${settings.language}`)}
            selectedOptions={[settings.language]}
            onOptionSelect={(_, data) => {
              if (data.optionValue && isLanguage(data.optionValue) && data.optionValue !== settings.language) {
                void save({ language: data.optionValue });
              }
            }}
          >
            {languages.map((lng) => (
              <Option key={lng} value={lng}>
                {t(`languageName.${lng}`)}
              </Option>
            ))}
          </Dropdown>
        </Field>
        <Field label={t("settings.theme")}>
          <RadioGroup layout="horizontal" value={settings.theme} onChange={(_, data) => void save({ theme: data.value })}>
            <Radio value="system" label={t("settings.themeSystem")} />
            <Radio value="light" label={t("settings.themeLight")} />
            <Radio value="dark" label={t("settings.themeDark")} />
          </RadioGroup>
        </Field>
      </Section>

      <Section title={t("settings.behavior")}>
        <Field label={t("settings.closeBehavior")}>
          <RadioGroup value={settings.closeBehavior} onChange={(_, data) => void save({ closeBehavior: data.value })}>
            <Radio value="tray" label={t("settings.closeToTray")} />
            <Radio value="quit" label={t("settings.closeQuit")} />
          </RadioGroup>
        </Field>
        <Caption1 className={styles.hint}>{t("settings.closeHint")}</Caption1>
      </Section>

      <Section title={t("settings.connection")}>
        <Field label={t("settings.localPort")} hint={t("settings.localPortHint")}>
          <TextSetting
            className={styles.port}
            value={String(settings.localPort)}
            check={(text) =>
              /^\d{1,5}$/.test(text.trim()) && Number(text) >= 1 && Number(text) <= 65535 ? undefined : t("settings.portRange")
            }
            onSave={(text) => void save({ localPort: Number(text) })}
          />
        </Field>
        <Switch
          checked={settings.checkRouteBeforeConnect}
          label={t("settings.checkFirst")}
          onChange={(_, d) => void save({ checkRouteBeforeConnect: d.checked })}
        />
        <Caption1 className={styles.hint}>{t("settings.checkFirstHint")}</Caption1>
      </Section>

      <Section title={t("settings.advanced")}>
        <Field label={t("settings.testUrl")} hint={t("settings.testUrlHint")}>
          <TextSetting
            className={styles.url}
            value={settings.testUrl}
            check={(text) => (/^https?:\/\/\S+$/i.test(text.trim()) ? undefined : t("settings.urlInvalid"))}
            onSave={(text) => void save({ testUrl: text.trim() })}
          />
        </Field>
        <Field label={t("settings.logLevel")} hint={t("settings.logLevelHint")}>
          <Dropdown
            className={styles.dropdown}
            value={t(`settings.logLevels.${settings.logLevel}`)}
            selectedOptions={[settings.logLevel]}
            onOptionSelect={(_, data) => data.optionValue && void save({ logLevel: data.optionValue })}
          >
            {["error", "warn", "info", "debug"].map((level) => (
              <Option key={level} value={level}>
                {t(`settings.logLevels.${level}`)}
              </Option>
            ))}
          </Dropdown>
        </Field>
      </Section>

      <Section title={t("settings.data")}>
        <Body1>{t("settings.dataIntro")}</Body1>
        {folders && (
          <div className={styles.folders}>
            <Body1 className={styles.label}>{t("settings.dataFolder")}</Body1>
            <Body1 className={styles.path}>{folders.data}</Body1>
            {openButton("data")}
            <div className={styles.files}>
              {dataFiles.map(([name, key]) => (
                <Fragment key={key}>
                  <Caption1 className={styles.file}>{name}</Caption1>
                  <Caption1 className={styles.hint}>{t(`settings.dataFiles.${key}`)}</Caption1>
                </Fragment>
              ))}
            </div>
            <Body1 className={styles.label}>{t("settings.logFolder")}</Body1>
            <Body1 className={styles.path}>{folders.logs}</Body1>
            {openButton("logs")}
          </div>
        )}
        <Caption1 className={styles.hint}>{t("settings.dataSecrets")}</Caption1>
      </Section>

      <Section title={t("settings.about")}>
        <div className={styles.about}>
          <Body1 className={styles.label}>{t("settings.version")}</Body1>
          <Body1>
            {appInfo?.name} {appInfo?.version}
          </Body1>
          <Body1 className={styles.label}>{t("settings.licenseLabel")}</Body1>
          <Body1>MIT</Body1>
          <Body1 className={styles.label}>{t("settings.homepage")}</Body1>
          <Link onClick={() => appInfo && void Browser.OpenURL(appInfo.repo)}>{appInfo?.repo}</Link>
          {appInfo?.xray && (
            <>
              <Body1 className={styles.label}>Xray-core</Body1>
              <Body1>
                {appInfo.xray} · MPL-2.0 ·{" "}
                <Link onClick={() => void Browser.OpenURL(appInfo.xraySource)}>{t("settings.source")}</Link>
              </Body1>
            </>
          )}
        </div>
        <Caption1 className={styles.hint}>{t("settings.thirdParty")}</Caption1>
      </Section>
    </Page>
  );
}
