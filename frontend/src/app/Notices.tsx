import {
  Button,
  MessageBar,
  MessageBarActions,
  MessageBarBody,
  Tooltip,
  makeStyles,
} from "@fluentui/react-components";
import { Dismiss20Regular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import type { Notice } from "../api/backend";
import { noticeText } from "../lib/messages";
import { useData } from "../stores/data";

const useStyles = makeStyles({
  stack: {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    padding: "16px 32px 0",
    maxWidth: "760px",
  },
});

function intent(n: Notice): "error" | "warning" | "info" {
  return n.level === "error" ? "error" : n.level === "warn" ? "warning" : "info";
}

/** Things the user should know about that no button of theirs caused; each stays until closed. */
export function Notices() {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const notices = useData((s) => s.notices);
  const dismiss = useData((s) => s.dismissNotice);
  if (notices.length === 0) return null;
  return (
    <div className={styles.stack}>
      {notices.map((n) => {
        const text = noticeText(i18n, n);
        const body = <MessageBarBody>{text}</MessageBarBody>;
        return (
          <MessageBar key={n.id} intent={intent(n)} layout="multiline">
            {n.message ? (
              <Tooltip content={n.message} relationship="description">
                {body}
              </Tooltip>
            ) : (
              body
            )}
            <MessageBarActions
              containerAction={
                <Button appearance="transparent" aria-label={t("common.close")} icon={<Dismiss20Regular />} onClick={() => dismiss(n.id)} />
              }
            />
          </MessageBar>
        );
      })}
    </div>
  );
}
