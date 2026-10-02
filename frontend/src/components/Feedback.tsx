import { useCallback, type ReactNode } from "react";
import {
  Button,
  Caption1,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Link,
  MessageBar,
  MessageBarBody,
  MessageBarTitle,
  Toast,
  ToastBody,
  ToastTitle,
  Toaster,
  makeStyles,
  tokens,
  useToastController,
} from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import { errorOf, type ErrorView } from "../api/backend";
import { errorDetails, errorText } from "../lib/messages";

const TOASTER_ID = "app-toaster";
let toastCount = 0;

const useStyles = makeStyles({
  details: {
    display: "block",
    marginTop: "4px",
    color: tokens.colorNeutralForeground3,
    wordBreak: "break-word",
  },
  danger: {
    backgroundColor: tokens.colorStatusDangerBackground3,
    ":hover": { backgroundColor: tokens.colorStatusDangerBackground3Hover },
    ":hover:active": { backgroundColor: tokens.colorStatusDangerBackground3Pressed },
  },
});

/** The one place toasts appear; rendered once by the shell. */
export function AppToaster() {
  return <Toaster toasterId={TOASTER_ID} position="bottom-end" pauseOnHover />;
}

/** Shows the result of something the user did: an error from a service call, or a short confirmation. */
export function useNotify() {
  const { dispatchToast, dismissToast } = useToastController(TOASTER_ID);
  const { t, i18n } = useTranslation();

  const error = useCallback(
    (e: unknown, title?: string) => {
      const view = errorOf(e);
      if (view.code === "cancelled") return;
      const details = errorDetails(view);
      const text = errorText(i18n, view);
      const toastId = `error-${++toastCount}`;
      // Errors stay until closed, so they cannot slip by unread.
      const close = (
        <Link as="button" onClick={() => dismissToast(toastId)}>
          {t("common.close")}
        </Link>
      );
      dispatchToast(
        <Toast>
          <ToastTitle action={close}>{title ?? text}</ToastTitle>
          {(title || (details && details !== text)) && (
            <ToastBody subtitle={details && details !== text ? details : undefined}>{title ? text : null}</ToastBody>
          )}
        </Toast>,
        { intent: "error", timeout: -1, toastId },
      );
    },
    [dispatchToast, dismissToast, i18n, t],
  );

  const success = useCallback(
    (text: string) => {
      dispatchToast(
        <Toast>
          <ToastTitle>{text}</ToastTitle>
        </Toast>,
        { intent: "success" },
      );
    },
    [dispatchToast],
  );

  return { error, success };
}

/** An error from a service call, inside a dialog or a form. */
export function ErrorBar({ error, title }: { error: ErrorView | null; title?: string }) {
  const styles = useStyles();
  const { i18n } = useTranslation();
  if (!error) return null;
  const text = errorText(i18n, error);
  const details = errorDetails(error);
  return (
    <MessageBar intent="error" layout="multiline">
      <MessageBarBody>
        {title && <MessageBarTitle>{title}</MessageBarTitle>}
        {title ? <div>{text}</div> : text}
        {details && details !== text && <Caption1 className={styles.details}>{details}</Caption1>}
      </MessageBarBody>
    </MessageBar>
  );
}

/** Asks before something that cannot be undone. */
export function ConfirmDialog({
  open,
  title,
  children,
  confirm,
  danger,
  busy,
  onClose,
  onConfirm,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  confirm: string;
  danger?: boolean;
  busy?: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const styles = useStyles();
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={(_, data) => !data.open && onClose()}>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>{title}</DialogTitle>
          <DialogContent>{children}</DialogContent>
          <DialogActions>
            <Button appearance="secondary" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              appearance="primary"
              className={danger ? styles.danger : undefined}
              disabled={busy}
              onClick={onConfirm}
            >
              {confirm}
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
