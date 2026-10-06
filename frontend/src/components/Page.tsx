import type { ReactElement, ReactNode } from "react";
import { Body1, Caption1, Subtitle1, Title3, makeStyles, mergeClasses, tokens } from "@fluentui/react-components";

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
    alignItems: "flex-start",
    justifyContent: "space-between",
    gap: "16px",
  },
  heading: {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
  },
  actions: {
    display: "flex",
    gap: "8px",
    flexShrink: 0,
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
  emptyCompact: {
    marginTop: "16px",
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

/** The frame every page uses: a title, an optional one-line note and buttons, then the content. */
export function Page({
  title,
  subtitle,
  actions,
  children,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const styles = useStyles();
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.heading}>
          <Title3 as="h1">{title}</Title3>
          {subtitle && <Caption1 className={styles.subtitle}>{subtitle}</Caption1>}
        </div>
        {actions && <div className={styles.actions}>{actions}</div>}
      </header>
      {children}
    </div>
  );
}

/**
 * Shown when a list has nothing in it yet. The page's buttons stay in its
 * header, where they always are. compact: it follows content of its own (a
 * list's built-in entries), so it needs less room above.
 */
export function EmptyState({
  icon,
  title,
  body,
  compact,
}: {
  icon: ReactElement<{ className?: string }>;
  title: string;
  body: string;
  compact?: boolean;
}) {
  const styles = useStyles();
  return (
    <div className={mergeClasses(styles.empty, compact && styles.emptyCompact)}>
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
