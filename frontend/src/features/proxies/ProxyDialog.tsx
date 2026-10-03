import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  Button,
  Caption1,
  Combobox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Divider,
  Dropdown,
  Field,
  Input,
  Link,
  MessageBar,
  MessageBarBody,
  MessageBarTitle,
  Option,
  Radio,
  RadioGroup,
  Spinner,
  Subtitle2,
  Textarea,
  Tooltip,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import type { CancellablePromise } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import {
  errorOf,
  ProxyService,
  type ErrorView,
  type LatencyResult,
  type LinkNote,
  type Proxy,
  type ProxyOptions,
  type ProxyView,
} from "../../api/backend";
import { ErrorBar } from "../../components/Feedback";
import { errorDetails, errorText, fieldCodes, fieldText, linkNoteText } from "../../lib/messages";
import { useData } from "../../stores/data";
import {
  editableKinds,
  fingerprints,
  grpcModes,
  kcpHeaders,
  kindName,
  networkName,
  networks,
  securities,
  securityName,
  shadowsocksCiphers,
  tcpHeaders,
  vlessFlows,
  vmessCiphers,
  xhttpModes,
} from "./names";
import {
  advancedOptions,
  changeKind,
  changeNetwork,
  emptyOptions,
  formField,
  fromForm,
  fromLink,
  hasAdvanced,
  keepsSecret,
  sameSecret,
  sections,
  splitServer,
  toForm,
  type FieldKey,
  type ProxyForm,
} from "./proxyForm";
import { profileNames, proxyUsage } from "./usage";

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
  section: {
    marginTop: "6px",
  },
  hint: {
    color: tokens.colorNeutralForeground3,
  },
  // Fluent inputs are about 180px wide by default; fields in a grid fill their column.
  fill: {
    width: "100%",
    minWidth: "0",
  },
  code: {
    fontFamily: tokens.fontFamilyMonospace,
    fontSize: tokens.fontSizeBase200,
  },
  notes: {
    margin: "4px 0 0",
    paddingLeft: "18px",
  },
  test: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    minWidth: 0,
  },
  good: { color: tokens.colorPaletteGreenForeground1 },
  bad: {
    color: tokens.colorStatusDangerForeground1,
    // Caption1 is a span; only a block of some kind keeps to its width.
    display: "inline-block",
    verticalAlign: "middle",
    maxWidth: "240px",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
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
  options: emptyOptions,
  outbound: "",
};

/** What the last share link left to say: that it was read, and its notes. */
type Imported = { notes: LinkNote[] };

type Test =
  | { kind: "idle" }
  | { kind: "running"; call: CancellablePromise<LatencyResult> }
  | { kind: "done"; ms: number }
  | { kind: "failed"; error: ErrorView };

/** Creates a proxy (view is null) or edits one. */
export function ProxyDialog({ view, onClose }: { view: ProxyView | null; onClose: () => void }) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const editing = view !== null;
  const [base, setBase] = useState<Proxy | null>(editing ? null : newProxy);
  const [form, setForm] = useState<ProxyForm | null>(editing ? null : toForm(newProxy));
  const [problems, setProblems] = useState<Partial<Record<FieldKey, string>>>({});
  const [error, setError] = useState<ErrorView | null>(null);
  const [saving, setSaving] = useState(false);
  const [advanced, setAdvanced] = useState(false);
  const [link, setLink] = useState("");
  const [linkError, setLinkError] = useState<ErrorView | null>(null);
  const [imported, setImported] = useState<Imported | null>(null);
  const [test, setTest] = useState<Test>({ kind: "idle" });
  const testing = useRef<CancellablePromise<LatencyResult> | null>(null);
  // While a connection runs through the proxy, the editor only shows it: that
  // session would go on with the old settings. It unlocks when they end.
  const profiles = useData((s) => s.profiles);
  const sessions = useData((s) => s.sessions);
  const connected = view ? proxyUsage(view.proxy.id, profiles, sessions).connected : [];
  const locked = connected.length > 0;

  // The list leaves out what the editor needs (the settings, the custom
  // outbound); read the proxy itself.
  useEffect(() => {
    if (!view) return;
    ProxyService.Get(view.proxy.id).then(
      (p) => {
        setBase(p);
        setForm(toForm(p));
        setAdvanced(hasAdvanced(p.options));
      },
      (e: unknown) => setError(errorOf(e)),
    );
  }, [view]);

  // Closing the editor stops a test still running.
  useEffect(
    () => () => {
      testing.current?.cancel();
    },
    [],
  );

  const hasSecret = !!view?.hasSecret;
  const supported = !form || (editableKinds as readonly string[]).includes(form.kind);

  /** Any change makes a test result, or a test still running, about other settings. */
  const change = (next: ProxyForm, cleared: FieldKey[]) => {
    setForm(next);
    setProblems((p) => {
      const rest = { ...p };
      for (const k of cleared) delete rest[k];
      return rest;
    });
    if (test.kind === "running") {
      testing.current = null;
      test.call.cancel();
    }
    if (test.kind !== "idle") setTest({ kind: "idle" });
  };
  const set = <K extends Exclude<keyof ProxyForm, "options">>(key: K, value: ProxyForm[K]) => {
    if (!form) return;
    // A password typed in replaces the saved one, cleared or not.
    const next = key === "password" ? { ...form, password: value as string, clearSecret: false } : { ...form, [key]: value };
    change(next, [key]);
  };
  const setOption = (key: keyof ProxyOptions, value: string) => {
    if (form) change({ ...form, options: { ...form.options, [key]: value } }, [`options.${key}`]);
  };
  const problem = (key: FieldKey) => fieldText(i18n, problems[key]);
  const validation = (key: FieldKey) => (problems[key] ? "error" : "none");

  /** Shows field problems from the Go side next to their fields; returns whether there were any. */
  const showProblems = (v: ErrorView): boolean => {
    const fields: Partial<Record<FieldKey, string>> = {};
    for (const [field, code] of Object.entries(fieldCodes(v.fields))) {
      const key = formField(field);
      if (key) fields[key] ??= code;
    }
    setProblems(fields);
    if (Object.keys(fields).some((k) => advancedOptions.some((a) => k === `options.${a}`))) setAdvanced(true);
    return v.code === "validation" && Object.keys(fields).length > 0;
  };

  const importLink = async () => {
    if (!form || link.trim() === "") return;
    setLinkError(null);
    try {
      const v = await ProxyService.ParseLink(link);
      const next = fromLink(form, v.proxy, editing ? base : null, hasSecret);
      change(next, Object.keys(problems) as FieldKey[]);
      setAdvanced(hasAdvanced(next.options));
      setImported({ notes: v.notes ?? [] });
      setLink("");
      setError(null);
    } catch (e) {
      setLinkError(errorOf(e));
      setImported(null);
    }
  };

  const save = async () => {
    if (!form || !base || locked) return;
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
      // Field problems show next to their fields; anything else above them.
      setError(showProblems(v) ? null : v);
    } finally {
      setSaving(false);
    }
  };

  const runTest = () => {
    if (test.kind === "running") {
      testing.current = null;
      test.call.cancel();
      setTest({ kind: "idle" });
      return;
    }
    if (!form || !base) return;
    const { proxy, keepSecret, problems: local } = fromForm(base, form, hasSecret);
    if (Object.keys(local).length > 0) {
      setProblems(local);
      return;
    }
    const call = ProxyService.DraftLatency(proxy, keepSecret);
    testing.current = call;
    setTest({ kind: "running", call });
    call.then(
      (r) => {
        if (testing.current !== call) return;
        testing.current = null;
        setTest({ kind: "done", ms: r.ms });
      },
      (e: unknown) => {
        if (testing.current !== call) return; // cancelled
        testing.current = null;
        const v = errorOf(e);
        if (showProblems(v)) setTest({ kind: "idle" });
        else setTest({ kind: "failed", error: v });
      },
    );
  };

  if (!form || !base) {
    // While the proxy loads there is no dialog yet: one that opens with
    // nothing to focus never gets Fluent's focus trap going, and then sends
    // focus out of the dialog each time it lands inside.
    if (!error) return null;
    return (
      <Dialog open onOpenChange={(_, d) => !d.open && onClose()}>
        <DialogSurface className={styles.surface}>
          <ErrorBar error={error} />
        </DialogSurface>
      </Dialog>
    );
  }

  const show = sections(form);
  const o = form.options;
  const secretSaved = keepsSecret(base, { ...form, password: "" }, hasSecret);

  /** A text field of the settings. */
  const text = (key: keyof ProxyOptions, label: string, extra: { placeholder?: string; hint?: string; password?: boolean; required?: boolean } = {}) => (
    <Field
      key={key}
      label={label}
      required={extra.required}
      hint={extra.hint}
      validationState={validation(`options.${key}`)}
      validationMessage={problem(`options.${key}`)}
    >
      <Input
        className={styles.fill}
        type={extra.password ? "password" : "text"}
        spellCheck={false}
        disabled={locked}
        value={o[key]}
        placeholder={extra.placeholder}
        onChange={(_, d) => setOption(key, d.value)}
      />
    </Field>
  );
  /** A JSON field of the settings. */
  const json = (key: keyof ProxyOptions, label: string, hint?: string) => (
    <Field key={key} label={label} hint={hint} validationState={validation(`options.${key}`)} validationMessage={problem(`options.${key}`)}>
      <Textarea
        textarea={{ className: styles.code }}
        spellCheck={false}
        disabled={locked}
        resize="vertical"
        rows={3}
        value={o[key]}
        placeholder="{ }"
        onChange={(_, d) => setOption(key, d.value)}
      />
    </Field>
  );
  /** A list of choices of the settings. */
  const choice = (key: keyof ProxyOptions, label: string, values: readonly string[], name: (v: string) => string = (v) => v) => (
    <Field key={key} label={label} validationState={validation(`options.${key}`)} validationMessage={problem(`options.${key}`)}>
      <Dropdown
        className={styles.fill}
        disabled={locked}
        value={name(o[key])}
        selectedOptions={[o[key]]}
        onOptionSelect={(_, d) => d.optionValue !== undefined && setOption(key, d.optionValue)}
      >
        {values.map((v) => (
          <Option key={v} value={v} text={name(v)}>
            {name(v)}
          </Option>
        ))}
      </Dropdown>
    </Field>
  );
  const none = (v: string) => (v === "none" || v === "" ? t("proxies.form.none") : v);

  const secretField = show.secret && (
    <Field
      label={show.secret === "userId" ? t("proxies.form.userId") : t("proxies.form.password")}
      required
      validationState={validation("password")}
      validationMessage={problem("password")}
      hint={show.secret === "userId" ? t("proxies.form.userIdHint") : undefined}
    >
      <Input
        className={styles.fill}
        // A user ID is pasted rather than typed; seeing it helps to check it.
        type={show.secret === "userId" ? "text" : "password"}
        spellCheck={false}
        disabled={locked}
        value={form.password}
        placeholder={secretSaved ? t("proxies.form.passwordSaved") : show.secret === "userId" ? "UUID" : undefined}
        onChange={(_, d) => set("password", d.value)}
      />
    </Field>
  );

  const transport = show.transport && (
    <>
      <Divider className={styles.section} />
      <Subtitle2>{t("proxies.form.transport")}</Subtitle2>
      <div className={styles.row}>
        <Field label={t("proxies.form.network")} validationState={validation("options.network")} validationMessage={problem("options.network")}>
          <Dropdown
            className={styles.fill}
            disabled={locked}
            value={networkName(t, o.network)}
            selectedOptions={[o.network]}
            onOptionSelect={(_, d) => {
              if (d.optionValue && form) {
                change({ ...form, options: changeNetwork(form.options, d.optionValue) }, ["options.network", "options.mode", "options.headerType"]);
              }
            }}
          >
            {networks.map((v) => (
              <Option key={v} value={v} text={networkName(t, v)}>
                {networkName(t, v)}
              </Option>
            ))}
          </Dropdown>
        </Field>
        {o.network === "tcp" && choice("headerType", t("proxies.form.disguise"), tcpHeaders, none)}
        {o.network === "kcp" && choice("headerType", t("proxies.form.disguise"), kcpHeaders, none)}
        {(o.network === "grpc" || o.network === "xhttp") &&
          choice("mode", t("proxies.form.mode"), o.network === "grpc" ? grpcModes : xhttpModes)}
      </div>
      {(o.network === "ws" || o.network === "httpupgrade" || o.network === "xhttp" || (o.network === "tcp" && o.headerType === "http")) && (
        <div className={styles.row}>
          {text("host", t("proxies.form.host"), { hint: o.network === "tcp" ? t("proxies.form.listHint") : undefined })}
          {text("path", t("proxies.form.path"), { placeholder: "/", hint: o.network === "tcp" ? t("proxies.form.listHint") : undefined })}
        </div>
      )}
      {o.network === "grpc" && text("serviceName", t("proxies.form.serviceName"))}
      {o.network === "kcp" && (
        <div className={styles.row}>
          {text("seed", t("proxies.form.seed"), { hint: t("proxies.form.seedHint") })}
          {o.headerType === "dns" && text("host", t("proxies.form.dnsDomain"))}
        </div>
      )}

      <Divider className={styles.section} />
      <Subtitle2>{t("proxies.form.security")}</Subtitle2>
      <RadioGroup layout="horizontal" value={o.security} disabled={locked} onChange={(_, d) => setOption("security", d.value)}>
        {securities.map((s) => (
          <Radio key={s} value={s} label={s === "none" ? t("proxies.form.none") : securityName(t, s)} />
        ))}
      </RadioGroup>
      {problem("options.security") && <Caption1 className={styles.bad}>{problem("options.security")}</Caption1>}
    </>
  );

  const tls = show.tls && (
    <>
      {form.kind === "hysteria2" && (
        <>
          <Divider className={styles.section} />
          <Subtitle2>{t("proxies.form.tls")}</Subtitle2>
        </>
      )}
      <div className={styles.row}>
        {text("sni", t("proxies.form.sni"), { placeholder: form.server || undefined, hint: t("proxies.form.sniHint") })}
        {text("alpn", t("proxies.form.alpn"), { placeholder: form.kind === "hysteria2" ? "h3" : "h2,http/1.1" })}
      </div>
      {form.kind !== "hysteria2" && fingerprintField()}
      {text("pinnedCerts", t("proxies.form.pinnedCerts"), { hint: t("proxies.form.pinnedCertsHint") })}
    </>
  );

  const reality = show.reality && (
    <>
      <div className={styles.row}>
        {text("sni", t("proxies.form.sni"), { hint: t("proxies.form.realitySniHint") })}
        {fingerprintField()}
      </div>
      <div className={styles.row}>
        {text("publicKey", t("proxies.form.publicKey"), { required: true })}
        {text("shortId", t("proxies.form.shortId"))}
      </div>
    </>
  );

  function fingerprintField() {
    return (
      <Field
        key="fingerprint"
        label={t("proxies.form.fingerprint")}
        validationState={validation("options.fingerprint")}
        validationMessage={problem("options.fingerprint")}
      >
        <Combobox
          className={styles.fill}
          freeform
          disabled={locked}
          value={o.fingerprint}
          selectedOptions={[o.fingerprint]}
          placeholder={t("proxies.form.fingerprintDefault")}
          onChange={(e) => setOption("fingerprint", e.target.value)}
          // Leaving the field also "selects", with no option; keep what was typed then.
          onOptionSelect={(_, d) => d.optionValue !== undefined && setOption("fingerprint", d.optionValue)}
        >
          {fingerprints.map((f) => (
            <Option key={f} value={f}>
              {f}
            </Option>
          ))}
        </Combobox>
      </Field>
    );
  }

  // Settings few links carry and fewer people change.
  const advancedFields: ReactNode[] = [];
  if (show.transport) {
    if (o.network === "grpc") advancedFields.push(text("authority", t("proxies.form.authority")));
    if (o.network === "xhttp") advancedFields.push(json("extra", t("proxies.form.extra")));
    advancedFields.push(json("finalMask", t("proxies.form.finalMask"), t("proxies.form.finalMaskHint")));
  }
  if (show.tls) advancedFields.push(text("verifyNames", t("proxies.form.verifyNames"), { hint: t("proxies.form.verifyNamesHint") }));
  if (show.tls && form.kind !== "hysteria2") advancedFields.push(text("ech", t("proxies.form.ech")));
  if (show.reality) {
    advancedFields.push(text("spiderX", t("proxies.form.spiderX"), { placeholder: "/" }));
    advancedFields.push(text("mldsa65Verify", t("proxies.form.mldsa65Verify")));
  }

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
              {locked && (
                <MessageBar intent="info" layout="multiline">
                  <MessageBarBody>{t("proxies.form.locked", { profiles: profileNames(connected, t("common.listSeparator")) })}</MessageBarBody>
                </MessageBar>
              )}
              <ErrorBar error={error} />
              {view?.secretsLost && (
                <MessageBar intent="warning" layout="multiline">
                  <MessageBarBody>{t("proxies.form.secretsLost")}</MessageBarBody>
                </MessageBar>
              )}
              {!supported && <Caption1 className={styles.hint}>{t("proxies.form.unsupportedKind")}</Caption1>}
              {supported && (
                <>
                  <Field
                    label={t("proxies.form.link")}
                    hint={editing ? t("proxies.form.linkHintEdit") : t("proxies.form.linkHint")}
                    validationState={linkError ? "error" : "none"}
                    validationMessage={linkError ? errorText(i18n, linkError) : undefined}
                  >
                    <Input
                      value={link}
                      autoFocus={!editing}
                      disabled={locked}
                      spellCheck={false}
                      placeholder="vless://… vmess://… ss://…"
                      onChange={(_, d) => {
                        setLink(d.value);
                        setLinkError(null);
                      }}
                      onKeyDown={(e) => {
                        // Enter reads the link rather than saving the form.
                        if (e.key === "Enter") {
                          e.preventDefault();
                          void importLink();
                        }
                      }}
                      contentAfter={
                        <Button
                          type="button"
                          size="small"
                          appearance="transparent"
                          disabled={link.trim() === "" || locked}
                          onClick={() => void importLink()}
                        >
                          {t("proxies.form.linkImport")}
                        </Button>
                      }
                    />
                  </Field>
                  {imported && (
                    <MessageBar intent={imported.notes.length > 0 ? "warning" : "success"} layout="multiline">
                      <MessageBarBody>
                        <MessageBarTitle>{t("proxies.form.linkImported")}</MessageBarTitle>
                        {imported.notes.length > 0 && (
                          <ul className={styles.notes}>
                            {imported.notes.map((n, i) => (
                              <li key={i}>{linkNoteText(i18n, n)}</li>
                            ))}
                          </ul>
                        )}
                      </MessageBarBody>
                    </MessageBar>
                  )}

                  <div className={styles.row}>
                    <Field label={t("proxies.form.kind")} validationState={validation("kind")} validationMessage={problem("kind")}>
                      <Dropdown
                        className={styles.fill}
                        disabled={locked}
                        value={kindName(t, form.kind)}
                        selectedOptions={[form.kind]}
                        onOptionSelect={(_, d) => {
                          if (!d.optionValue || d.optionValue === form.kind) return;
                          change(changeKind(form, d.optionValue), ["kind"]);
                          // What was wrong with the other kind says nothing about this one.
                          setError(null);
                        }}
                      >
                        {editableKinds.map((k) => (
                          <Option key={k} value={k} text={kindName(t, k)}>
                            {kindName(t, k)}
                          </Option>
                        ))}
                      </Dropdown>
                    </Field>
                    <Field label={t("proxies.form.name")} required validationState={validation("name")} validationMessage={problem("name")}>
                      <Input className={styles.fill} value={form.name} disabled={locked} onChange={(_, d) => set("name", d.value)} />
                    </Field>
                  </div>
                  <div className={styles.server}>
                    <Field
                      label={t("proxies.form.server")}
                      required={!show.custom}
                      validationState={validation("server")}
                      validationMessage={problem("server")}
                      hint={show.custom ? t("proxies.form.customServerHint") : t("proxies.form.serverHint")}
                    >
                      <Input
                        value={form.server}
                        disabled={locked}
                        spellCheck={false}
                        placeholder={form.kind === "socks" || form.kind === "http" ? "127.0.0.1" : "proxy.example.com"}
                        onChange={(_, d) => set("server", d.value)}
                        onBlur={() => {
                          const split = splitServer(form);
                          if (split === form) return;
                          change(split, ["server", "port"]);
                        }}
                      />
                    </Field>
                    <Field label={t("proxies.form.port")} required={!show.custom} validationState={validation("port")} validationMessage={problem("port")}>
                      <Input
                        className={styles.fill}
                        value={form.port}
                        disabled={locked}
                        inputMode="numeric"
                        placeholder={form.kind === "http" ? "8080" : form.kind === "socks" ? "1080" : "443"}
                        onChange={(_, d) => set("port", d.value)}
                      />
                    </Field>
                  </div>

                  {show.account && (
                    <>
                      <div className={styles.row}>
                        <Field label={t("proxies.form.username")} validationState={validation("username")} validationMessage={problem("username")}>
                          <Input className={styles.fill} value={form.username} disabled={locked} onChange={(_, d) => set("username", d.value)} />
                        </Field>
                        <Field label={t("proxies.form.password")} validationState={validation("password")} validationMessage={problem("password")}>
                          <Input
                            className={styles.fill}
                            type="password"
                            value={form.password}
                            disabled={form.clearSecret || locked}
                            placeholder={form.clearSecret ? t("proxies.form.passwordCleared") : secretSaved ? t("proxies.form.passwordSaved") : undefined}
                            onChange={(_, d) => set("password", d.value)}
                          />
                        </Field>
                      </div>
                      {hasSecret && sameSecret(base.kind, form.kind) && (
                        <Link
                          as="button"
                          type="button"
                          disabled={locked}
                          onClick={() => change({ ...form, clearSecret: !form.clearSecret, password: "" }, ["password"])}
                        >
                          {form.clearSecret ? t("proxies.form.keepPassword") : t("proxies.form.clearPassword")}
                        </Link>
                      )}
                    </>
                  )}

                  {form.kind === "shadowsocks" && (
                    <div className={styles.row}>
                      {choice("cipher", t("proxies.form.method"), shadowsocksCiphers)}
                      {secretField}
                    </div>
                  )}
                  {form.kind === "vmess" && (
                    <div className={styles.row}>
                      {secretField}
                      {choice("cipher", t("proxies.form.vmessCipher"), vmessCiphers, (v) => (v === "auto" ? t("proxies.form.auto") : none(v)))}
                    </div>
                  )}
                  {form.kind === "vless" && (
                    <>
                      {secretField}
                      <div className={styles.row}>
                        <Field label={t("proxies.form.flow")} validationState={validation("options.flow")} validationMessage={problem("options.flow")}>
                          <Dropdown
                            className={styles.fill}
                            disabled={locked}
                            value={none(o.flow)}
                            selectedOptions={[o.flow || "none"]}
                            // Fluent cannot hold an empty value; "none" stands for no flow.
                            onOptionSelect={(_, d) => d.optionValue !== undefined && setOption("flow", d.optionValue === "none" ? "" : d.optionValue)}
                          >
                            {vlessFlows.map((f) => (
                              <Option key={f || "none"} value={f || "none"} text={none(f)}>
                                {none(f)}
                              </Option>
                            ))}
                          </Dropdown>
                        </Field>
                        {text("encryption", t("proxies.form.encryption"), { placeholder: "none", hint: t("proxies.form.encryptionHint") })}
                      </div>
                    </>
                  )}
                  {(form.kind === "trojan" || form.kind === "hysteria2") && secretField}
                  {form.kind === "hysteria2" &&
                    text("obfsPassword", t("proxies.form.obfsPassword"), { password: true, hint: t("proxies.form.obfsPasswordHint") })}

                  {transport}
                  {tls}
                  {reality}

                  {show.custom && (
                    <Field
                      label={t("proxies.form.outbound")}
                      required
                      hint={t("proxies.form.outboundHint")}
                      validationState={validation("outbound")}
                      validationMessage={problem("outbound")}
                    >
                      <Textarea
                        textarea={{ className: styles.code }}
                        spellCheck={false}
                        disabled={locked}
                        resize="vertical"
                        rows={12}
                        value={form.outbound}
                        placeholder={'{\n  "protocol": "vless",\n  "settings": { … },\n  "streamSettings": { … }\n}'}
                        onChange={(_, d) => set("outbound", d.value)}
                      />
                    </Field>
                  )}

                  {advancedFields.length > 0 && (
                    <>
                      {/* Locked, there is nothing to unfold: settings with a value open unfolded (hasAdvanced).
                          And the link would be the one thing to focus, which scrolls the editor to its end. */}
                      {!locked && (
                        <Link as="button" type="button" className={styles.section} onClick={() => setAdvanced(!advanced)}>
                          {advanced ? t("proxies.form.hideAdvanced") : t("proxies.form.showAdvanced")}
                        </Link>
                      )}
                      {advanced && advancedFields}
                    </>
                  )}
                </>
              )}
            </DialogContent>
            <DialogActions position="start">
              <div className={styles.test}>
                <Tooltip content={t("proxies.latencyHint")} relationship="description">
                  <Button type="button" disabled={!supported} onClick={runTest}>
                    {test.kind === "running" ? t("common.cancel") : t("proxies.form.test")}
                  </Button>
                </Tooltip>
                {test.kind === "running" && <Spinner size="extra-tiny" />}
                {test.kind === "done" && <Caption1 className={styles.good}>{t("proxies.latencyMs", { ms: test.ms })}</Caption1>}
                {test.kind === "failed" && (
                  <Tooltip content={errorDetails(test.error) || errorText(i18n, test.error)} relationship="description">
                    <Caption1 className={styles.bad}>{errorText(i18n, test.error)}</Caption1>
                  </Tooltip>
                )}
              </div>
            </DialogActions>
            <DialogActions>
              <Button appearance="secondary" type="button" onClick={onClose}>
                {locked ? t("common.close") : t("common.cancel")}
              </Button>
              <Button appearance="primary" type="submit" disabled={saving || !supported || locked}>
                {t("common.save")}
              </Button>
            </DialogActions>
          </DialogBody>
        </form>
      </DialogSurface>
    </Dialog>
  );
}
