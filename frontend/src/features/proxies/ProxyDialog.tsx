import { useEffect, useState } from "react";
import {
  Button,
  Caption1,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Field,
  Input,
  Link,
  Radio,
  RadioGroup,
  Spinner,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import { errorOf, ProxyService, type ErrorView, type Proxy, type ProxyView } from "../../api/backend";
import { ErrorBar } from "../../components/Feedback";
import { fieldCodes, fieldText } from "../../lib/messages";
import { editableKinds, kindName } from "./names";
import { formField, fromForm, splitServer, toForm, type ProxyForm } from "./proxyForm";

const useStyles = makeStyles({
  surface: {
    width: "520px",
    maxWidth: "calc(100vw - 48px)",
  },
  content: {
    display: "flex",
    flexDirection: "column",
    gap: "14px",
  },
  server: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) 120px",
    alignItems: "start",
    gap: "12px",
  },
  row: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
    alignItems: "start",
    gap: "12px",
  },
  hint: {
    color: tokens.colorNeutralForeground3,
  },
  // Fluent inputs are about 180px wide by default; the port column is narrower.
  fill: {
    width: "100%",
    minWidth: "0",
  },
});

const newProxy: Proxy = {
  schema: 1,
  id: "",
  name: "",
  kind: "socks",
  server: "",
  port: 0,
  username: "",
  secret: "",
  outbound: "",
};

/** Creates a proxy (view is null) or edits a SOCKS5 or HTTP one. */
export function ProxyDialog({ view, onClose }: { view: ProxyView | null; onClose: () => void }) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const editing = view !== null;
  const [base, setBase] = useState<Proxy | null>(editing ? null : newProxy);
  const [form, setForm] = useState<ProxyForm | null>(editing ? null : toForm(newProxy));
  const [problems, setProblems] = useState<Record<string, string>>({});
  const [error, setError] = useState<ErrorView | null>(null);
  const [saving, setSaving] = useState(false);

  // The list leaves out what the editor needs (the Xray outbound); read the
  // proxy itself.
  useEffect(() => {
    if (!view) return;
    ProxyService.Get(view.proxy.id).then(
      (p) => {
        setBase(p);
        setForm(toForm(p));
      },
      (e: unknown) => setError(errorOf(e)),
    );
  }, [view]);

  const set = <K extends keyof ProxyForm>(key: K, value: ProxyForm[K]) => {
    if (!form) return;
    setForm({ ...form, [key]: value });
    setProblems(({ [key]: _, ...rest }) => rest);
  };
  const problem = (key: keyof ProxyForm) => fieldText(i18n, problems[key]);
  const validation = (key: keyof ProxyForm) => (problems[key] ? "error" : "none");
  const hasSecret = !!view?.hasSecret;

  const save = async () => {
    if (!form || !base) return;
    const { proxy, keepSecret, problems: local } = fromForm(base, form, hasSecret);
    if (Object.keys(local).length > 0) {
      setProblems(local);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      if (editing) await ProxyService.Update(proxy, keepSecret);
      else await ProxyService.Create(proxy);
      onClose();
    } catch (e) {
      const v = errorOf(e);
      const fields: Record<string, string> = {};
      for (const [field, code] of Object.entries(fieldCodes(v.fields))) {
        const key = formField(field);
        if (key) fields[key] ??= code;
      }
      setProblems(fields);
      setError(v.code === "validation" && Object.keys(fields).length > 0 ? null : v);
    } finally {
      setSaving(false);
    }
  };

  const supported = !form || (editableKinds as readonly string[]).includes(form.kind);

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
            <DialogTitle>{editing ? t("proxies.form.editTitle") : t("proxies.form.newTitle")}</DialogTitle>
            <DialogContent className={styles.content}>
              <ErrorBar error={error} />
              {!form && !error && <Spinner />}
              {form && !supported && <Caption1 className={styles.hint}>{t("proxies.form.unsupportedKind")}</Caption1>}
              {form && supported && (
                <>
                  <Field label={t("proxies.form.kind")}>
                    <RadioGroup layout="horizontal" value={form.kind} onChange={(_, d) => set("kind", d.value)}>
                      {editableKinds.map((k) => (
                        <Radio key={k} value={k} label={kindName(t, k)} />
                      ))}
                    </RadioGroup>
                  </Field>
                  <Field label={t("proxies.form.name")} required validationState={validation("name")} validationMessage={problem("name")}>
                    <Input value={form.name} autoFocus={!editing} onChange={(_, d) => set("name", d.value)} />
                  </Field>
                  <div className={styles.server}>
                    <Field
                      label={t("proxies.form.server")}
                      required
                      validationState={validation("server")}
                      validationMessage={problem("server")}
                    >
                      <Input
                        value={form.server}
                        placeholder="127.0.0.1"
                        onChange={(_, d) => set("server", d.value)}
                        onBlur={() => {
                          const split = splitServer(form);
                          if (split === form) return;
                          setForm(split);
                          setProblems(({ server: _s, port: _p, ...rest }) => rest);
                        }}
                      />
                    </Field>
                    <Field label={t("proxies.form.port")} required validationState={validation("port")} validationMessage={problem("port")}>
                      <Input
                        className={styles.fill}
                        value={form.port}
                        inputMode="numeric"
                        placeholder={form.kind === "http" ? "8080" : "1080"}
                        onChange={(_, d) => set("port", d.value)}
                      />
                    </Field>
                  </div>
                  <Caption1 className={styles.hint}>{t("proxies.form.serverHint")}</Caption1>
                  <div className={styles.row}>
                    <Field label={t("proxies.form.username")} validationState={validation("username")} validationMessage={problem("username")}>
                      <Input value={form.username} onChange={(_, d) => set("username", d.value)} />
                    </Field>
                    <Field
                      label={t("proxies.form.password")}
                      validationState={validation("password")}
                      validationMessage={problem("password")}
                    >
                      <Input
                        type="password"
                        value={form.password}
                        disabled={form.clearSecret}
                        placeholder={
                          form.clearSecret
                            ? t("proxies.form.passwordCleared")
                            : hasSecret
                              ? t("proxies.form.passwordSaved")
                              : undefined
                        }
                        onChange={(_, d) => set("password", d.value)}
                      />
                    </Field>
                  </div>
                  {hasSecret && (
                    <Link
                      as="button"
                      type="button"
                      onClick={() => setForm({ ...form, clearSecret: !form.clearSecret, password: "" })}
                    >
                      {form.clearSecret ? t("proxies.form.keepPassword") : t("proxies.form.clearPassword")}
                    </Link>
                  )}
                </>
              )}
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" type="button" onClick={onClose}>
                {t("common.cancel")}
              </Button>
              <Button appearance="primary" type="submit" disabled={saving || !form || !supported}>
                {t("common.save")}
              </Button>
            </DialogActions>
          </DialogBody>
        </form>
      </DialogSurface>
    </Dialog>
  );
}
