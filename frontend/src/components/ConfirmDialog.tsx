import { clsx } from "clsx";
import { AlertTriangle } from "lucide-react";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "danger" | "default";
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  variant = "default",
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/60" onClick={onCancel} />

      {/* Dialog */}
      <div className="relative bg-dark-grey border border-gray-800 rounded-xl shadow-2xl p-6 max-w-sm w-full mx-4 animate-fade-in">
        <div className="flex items-start gap-3 mb-4">
          {variant === "danger" && (
            <div className="p-2 bg-error/10 rounded-lg shrink-0">
              <AlertTriangle className="w-5 h-5 text-error" />
            </div>
          )}
          <div>
            <h3 className="text-sm font-semibold text-white">{title}</h3>
            <p className="text-sm text-gray-400 mt-1 leading-relaxed">{message}</p>
          </div>
        </div>

        <div className="flex items-center justify-end gap-2">
          <button
            onClick={onCancel}
            className="px-4 py-2 text-sm text-gray-400 hover:text-gray-200 hover:bg-gray-800 rounded-lg transition-colors"
          >
            {cancelLabel}
          </button>
          <button
            onClick={onConfirm}
            className={clsx(
              "px-4 py-2 text-sm font-medium rounded-lg transition-colors",
              variant === "danger"
                ? "bg-error/20 text-error hover:bg-error/30 border border-error/30"
                : "bg-primary/20 text-primary hover:bg-primary/30 border border-primary/30"
            )}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
