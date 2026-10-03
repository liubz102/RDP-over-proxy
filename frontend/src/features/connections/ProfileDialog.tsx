import { useEffect, useMemo, useState } from "react";
import {
  Button,
  Caption1,
  Checkbox,
  Combobox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Divider,
  Field,
  Input,
  Link,
  MessageBar,
  MessageBarBody,
  Option,
  Radio,
  RadioGroup,
  Spinner,
  Subtitle2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import { errorOf, ProfileService, type ErrorView, type Profile, type ProfileView } from "../../api/backend";
import { ErrorBar, useNotify } from "../../components/Feedback";
import { fieldCodes, fieldText } from "../../lib/messages";
import { useData } from "../../stores/data";
import { useEditDefaults } from "../diagnostics/editDefaults";
import { formField, fromForm, toForm, type ProfileForm } from "./profileForm";
import type { Imported } from "./rdpImport";
import { isActive } from "./status";

const useStyles = makeStyles({
  surface: {
    width: "600px",
    maxWidth: "calc(100vw - 48px)",
  },
  content: {
    display: "flex",
    flexDirection: "column",
    gap: "14px",
    maxHeight: "calc(100vh - 220px)",
    overflowY: "auto",
    paddingRight: "8px",
    // The content scrolls; without this its items would shrink to fit and overlap.
    "& > *": { flexShrink: 0 },
  },
  row: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
    alignItems: "start",
    gap: "12px",
  },
  size: {
    display: "flex",
    alignItems: "flex-start",
    gap: "8px",
  },
  sizeInput: {
    width: "110px",
  },
  section: {
    marginTop: "6px",
  },
  hint: {
    color: tokens.colorNeutralForeground3,
  },
  inline: {
    display: "flex",
    alignItems: "center",
    gap: "12px",
    flexWrap: "wrap",
  },
});

/**
 * Creates a profile (view is null), from scratch or from an imported .rdp
 * file, or edits one.
 */
export function ProfileDialog({
  view,
  imported,
  onClose,
}: {
  view: ProfileView | null;
  imported?: Imported;
  onClose: () => void;
}) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const notify = useNotify();
  const editDefaults = useEditDefaults();
  const proxies = useData((s) => s.proxies);
  const profiles = useData((s) => s.profiles);
  const [base, setBase] = useState<Profile | null>(view?.profile ?? null);
  const [form, setForm] = useState<ProfileForm | null>(view ? toForm(view.profile) : null);
  const [problems, setProblems] = useState<Record<string, string>>({});
  const [error, setError] = useState<ErrorView | null>(null);
  const [saving, setSaving] = useState(false);
  // Live: forgetting the password, or mstsc remembering one, updates it.
  const current = useData((s) => (view ? s.profiles.find((p) => p.profile.id === view.profile.id) : undefined));
  const savedNow = !!current && (current.passwordSaved || current.passwordByMstsc);
  // A connected profile only shows its settings; they unlock when the session ends.
  const locked = useData((s) => view !== null && isActive(s.sessions[view.profile.id]));

  // A new profile starts from the Go side's defaults, or from what an .rdp
  // file says, with the first proxy the user made (or direct, when there is
  // none). The list changes it later.
  useEffect(() => {
    if (view) return;
    const start = (draft: Profile) => {
      const firstProxy = proxies.find((p) => !p.builtIn) ?? proxies[0];
      const p = { ...draft, proxyId: firstProxy?.proxy.id ?? "direct" };
      setBase(p);
      setForm(toForm(p));
    };
    if (imported) {
      start(imported.view.profile);
      return;
    }
    ProfileService.Draft().then(start, (e: unknown) => setError(errorOf(e)));
    // Only once, when the dialog opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const groups = useMemo(
    () => [...new Set(profiles.map((p) => p.profile.group).filter((g) => g !== ""))].sort(),
    [profiles],
  );

  if (!form || !base) {
    return (
      <Dialog open onOpenChange={(_, d) => !d.open && onClose()}>
        <DialogSurface className={styles.surface}>
          {error ? <ErrorBar error={error} /> : <Spinner />}
        </DialogSurface>
      </Dialog>
    );
  }

  const set = <K extends keyof ProfileForm>(key: K, value: ProfileForm[K]) => {
    setForm({ ...form, [key]: value });
    setProblems(({ [key]: _, ...rest }) => rest);
  };
  const problem = (key: keyof ProfileForm) => fieldText(i18n, problems[key]);
  const validation = (key: keyof ProfileForm) => (problems[key] ? "error" : "none");

  const editing = view !== null;
  const addressChanged = editing && form.address !== toForm(base).address;

  const save = async () => {
    if (locked) return;
    const { profile, problems: local } = fromForm(base, form);
    const mapped: Record<string, string> = {};
    for (const [field, code] of Object.entries(local)) {
      const key = formField(field);
      if (key) mapped[key] ??= code;
    }
    if (Object.keys(mapped).length > 0) {
      setProblems(mapped);
      return;
    }
    setSaving(true);
    setError(null);
    const password = form.rememberPassword ? form.password : "";
    try {
      if (editing) await ProfileService.Update(profile, password);
      else await ProfileService.Create(profile, password);
      onClose();
    } catch (e) {
      const v = errorOf(e);
      const fields: Record<string, string> = {};
      for (const [field, code] of Object.entries(fieldCodes(v.fields))) {
        const key = formField(field);
        if (key) fields[key] ??= code;
      }
      setProblems(fields);
      // Field problems show next to their fields; anything else above them.
      // The proxy has no field here (the list chooses it): a problem with it,
      // such as one from a file edited by hand, says to choose another.
      if (v.fields?.some((f) => f.field === "proxyId")) setError({ code: "profile.proxyMissing", message: "" });
      else setError(v.code === "validation" && Object.keys(fields).length > 0 ? null : v);
    } finally {
      setSaving(false);
    }
  };

  const forget = async () => {
    if (!view) return;
    try {
      await ProfileService.ForgetPassword(view.profile.id);
      notify.success(t("connections.form.forgotten"));
    } catch (e) {
      notify.error(e);
    }
  };

  return (
    <Dialog open onOpenChange={(_, d) => !d.open && onClose()}>
      <DialogSurface className={styles.surface}>
        <form
          // The Go side checks the fields and says what is wrong with each;
          // the browser's own required-field bubbles would get in first.
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <DialogBody>
            <DialogTitle>
              {editing
                ? t("connections.form.editTitle")
                : imported
                  ? t("connections.import.title")
                  : t("connections.form.newTitle")}
            </DialogTitle>
            <DialogContent className={styles.content}>
              {locked && (
                <MessageBar intent="info" layout="multiline">
                  <MessageBarBody>{t("connections.form.locked")}</MessageBarBody>
                </MessageBar>
              )}
              {imported && <ImportNotes imported={imported} />}
              <ErrorBar error={error} />
              <div className={styles.row}>
                <Field label={t("connections.form.name")} required validationState={validation("name")} validationMessage={problem("name")}>
                  <Input value={form.name} disabled={locked} onChange={(_, d) => set("name", d.value)} autoFocus={!editing} />
                </Field>
                <Field label={t("connections.form.group")} validationState={validation("group")} validationMessage={problem("group")}>
                  <Combobox
                    freeform
                    disabled={locked}
                    value={form.group}
                    selectedOptions={[form.group]}
                    placeholder={t("connections.form.groupPlaceholder")}
                    onChange={(e) => set("group", e.target.value)}
                    // Leaving the field also "selects", with no option; keep what was typed then.
                    onOptionSelect={(_, d) => d.optionValue !== undefined && set("group", d.optionValue)}
                  >
                    {groups.map((g) => (
                      <Option key={g} value={g}>
                        {g}
                      </Option>
                    ))}
                  </Combobox>
                </Field>
              </div>
              <Field
                label={t("connections.form.address")}
                required
                validationState={validation("address")}
                validationMessage={
                  problem("address") ?? (addressChanged && savedNow ? t("connections.form.addressChanged") : undefined)
                }
                hint={t("connections.form.addressHint")}
              >
                <Input
                  value={form.address}
                  disabled={locked}
                  placeholder="pc.example.com"
                  onChange={(_, d) => set("address", d.value)}
                />
              </Field>

              <Divider className={styles.section} />
              <Subtitle2>{t("connections.form.signIn")}</Subtitle2>
              <Field
                label={t("connections.form.username")}
                validationState={validation("username")}
                validationMessage={problem("username")}
                hint={t("connections.form.usernameHint")}
              >
                <Input value={form.username} disabled={locked} placeholder="DOMAIN\user" onChange={(_, d) => set("username", d.value)} />
              </Field>
              <Checkbox
                checked={form.rememberPassword}
                disabled={locked}
                label={t("connections.form.remember")}
                onChange={(_, d) => set("rememberPassword", !!d.checked)}
              />
              {form.rememberPassword ? (
                <Field
                  label={t("connections.form.password")}
                  validationState={validation("password")}
                  validationMessage={problem("password")}
                  hint={
                    savedNow
                      ? t("connections.form.passwordKeep")
                      : t("connections.form.passwordHint")
                  }
                >
                  <Input
                    type="password"
                    value={form.password}
                    disabled={locked}
                    placeholder={savedNow ? t("connections.form.passwordSaved") : undefined}
                    onChange={(_, d) => set("password", d.value)}
                  />
                </Field>
              ) : (
                <Caption1 className={styles.hint}>{t("connections.form.noRememberHint")}</Caption1>
              )}
              {editing && savedNow && (
                <div className={styles.inline}>
                  <Caption1 className={styles.hint}>
                    {current?.passwordByMstsc && !current.passwordSaved
                      ? t("connections.form.savedByMstsc")
                      : t("connections.form.savedByApp")}
                  </Caption1>
                  <Link as="button" type="button" disabled={locked} onClick={() => void forget()}>
                    {t("connections.form.forget")}
                  </Link>
                </div>
              )}

              <Divider className={styles.section} />
              <Subtitle2>{t("connections.form.display")}</Subtitle2>
              <RadioGroup value={form.mode} disabled={locked} onChange={(_, d) => set("mode", d.value)}>
                <Radio value="default" label={t("connections.form.modeDefault")} />
                <Radio value="fullscreen" label={t("connections.form.modeFullscreen")} />
                <Radio value="window" label={t("connections.form.modeWindow")} />
              </RadioGroup>
              {form.mode === "fullscreen" && (
                <Field label={t("connections.form.screens")} validationState={validation("screens")} validationMessage={problem("screens")}>
                  <RadioGroup value={form.screens} disabled={locked} onChange={(_, d) => set("screens", d.value as ProfileForm["screens"])}>
                    <Radio value="one" label={t("connections.form.screensOne")} />
                    <Radio value="multimon" label={t("connections.form.screensMultimon")} />
                    <Radio value="span" label={t("connections.form.screensSpan")} />
                  </RadioGroup>
                </Field>
              )}
              {form.mode === "window" && (
                <div className={styles.size}>
                  <Field label={t("connections.form.width")} validationState={validation("width")} validationMessage={problem("width")}>
                    <Input className={styles.sizeInput} inputMode="numeric" value={form.width} disabled={locked} onChange={(_, d) => set("width", d.value)} />
                  </Field>
                  <Field label={t("connections.form.height")} validationState={validation("height")} validationMessage={problem("height")}>
                    <Input className={styles.sizeInput} inputMode="numeric" value={form.height} disabled={locked} onChange={(_, d) => set("height", d.value)} />
                  </Field>
                </div>
              )}
              {form.mode === "window" && <Caption1 className={styles.hint}>{t("connections.form.sizeHint")}</Caption1>}
              <Caption1 className={styles.hint}>
                {t("connections.form.displayHint")}{" "}
                <Link as="button" type="button" inline onClick={editDefaults}>
                  {t("diag.editDefaults")}
                </Link>
              </Caption1>
              <Checkbox checked={form.admin} disabled={locked} label={t("connections.form.admin")} onChange={(_, d) => set("admin", !!d.checked)} />
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" type="button" onClick={onClose}>
                {locked ? t("common.close") : t("common.cancel")}
              </Button>
              <Button appearance="primary" type="submit" disabled={saving || locked}>
                {t("common.save")}
              </Button>
            </DialogActions>
          </DialogBody>
        </form>
      </DialogSurface>
    </Dialog>
  );
}

/** What reading the .rdp file found, above the form it filled in. */
function ImportNotes({ imported }: { imported: Imported }) {
  const { t } = useTranslation();
  const v = imported.view;
  return (
    <>
      <MessageBar intent="info" layout="multiline">
        <MessageBarBody>{t("connections.import.read", { file: imported.file })}</MessageBarBody>
      </MessageBar>
      {v.viaGateway && (
        <MessageBar intent="warning" layout="multiline">
          <MessageBarBody>
            {v.gateway ? t("connections.import.gateway", { server: v.gateway }) : t("connections.import.gatewayUnnamed")}
          </MessageBarBody>
        </MessageBar>
      )}
      {(v.existing ?? []).length > 0 && (
        <MessageBar intent="warning" layout="multiline">
          <MessageBarBody>
            {t("connections.import.existing", { names: (v.existing ?? []).join(t("common.listSeparator")) })}
          </MessageBarBody>
        </MessageBar>
      )}
    </>
  );
}
