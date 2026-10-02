import type { ReactElement, ReactNode } from "react";
import { Body1, Caption1, Subtitle1, Title3, makeStyles, tokens } from "@fluentui/react-components";

const useStyles = makeStyles({
  page: {
    maxWidth: "760px",
    padding: "28px 32px 32px",
    display: "flex",
    flexDirection: "column",
    gap: "20px",
  },
  header: {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
  },
  subtitle: {
    color: tokens.colorNeutralForeground3,
  },
  empty: {
    marginTop: "48px",
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    textAlign: "center",
    gap: "8px",
  },
  emptyIcon: {
    fontSize: "48px",
    color: tokens.colorNeutralForeground3,
    marginBottom: "8px",
  },
  emptyBody: {
    maxWidth: "440px",
    color: tokens.colorNeutralForeground3,
  },
});

/** The frame every page uses: a title, an optional one-line note, then the content. */
export function Page({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  const styles = useStyles();
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Title3 as="h1">{title}</Title3>
        {subtitle && <Caption1 className={styles.subtitle}>{subtitle}</Caption1>}
      </header>
      {children}
    </div>
  );
}

/** Shown when a list has nothing in it yet. */
export function EmptyState({ icon, title, body }: { icon: ReactElement<{ className?: string }>; title: string; body: string }) {
  const styles = useStyles();
  return (
    <div className={styles.empty}>
      <span className={styles.emptyIcon}>{icon}</span>
      <Subtitle1 as="h2" align="center">
        {title}
      </Subtitle1>
      {/* Fluent text components set their own text-align, so centring the container is not enough. */}
      <Body1 className={styles.emptyBody} align="center">
        {body}
      </Body1>
    </div>
  );
}
