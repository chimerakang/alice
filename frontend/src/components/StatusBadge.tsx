import { clsx } from "clsx";

type Variant = "success" | "error" | "warning" | "info" | "neutral";

type Size = "sm" | "md";

interface StatusBadgeProps {
  variant: Variant;
  children: React.ReactNode;
  dot?: boolean;
  size?: Size;
  className?: string;
  title?: string;
}

const styles: Record<Variant, string> = {
  success: "bg-success/15 text-success border-success/30",
  error: "bg-error/15 text-error border-error/30",
  warning: "bg-warning/15 text-warning border-warning/30",
  info: "bg-primary/15 text-primary border-primary/30",
  neutral: "bg-gray-700/50 text-gray-400 border-gray-600/30",
};

export default function StatusBadge({
  variant,
  children,
  dot = false,
  size = "md",
  className,
  title,
}: StatusBadgeProps) {
  return (
    <span
      title={title}
      className={clsx(
        "inline-flex items-center font-medium rounded-full border",
        size === "sm" ? "gap-1 px-1.5 py-px text-[10px]" : "gap-1.5 px-2 py-0.5 text-xs",
        styles[variant],
        className
      )}
    >
      {dot && (
        <span
          className={clsx("w-1.5 h-1.5 rounded-full", {
            "bg-success": variant === "success",
            "bg-error": variant === "error",
            "bg-warning": variant === "warning",
            "bg-primary": variant === "info",
            "bg-gray-400": variant === "neutral",
          })}
        />
      )}
      {children}
    </span>
  );
}
