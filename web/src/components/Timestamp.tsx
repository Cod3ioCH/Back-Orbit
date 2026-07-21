import { formatDateTime, formatRelativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

interface TimestampProps {
  iso: string;
  className?: string;
}

/**
 * Timestamp renders a relative time ("2 minutes ago") — which is what someone
 * scanning a list actually wants — while keeping the exact time available on
 * hover for when precision matters. Using this everywhere keeps time
 * formatting consistent across the app.
 */
export function Timestamp({ iso, className }: TimestampProps) {
  return (
    <time
      dateTime={iso}
      title={formatDateTime(iso)}
      className={cn("tabular-nums", className)}
    >
      {formatRelativeTime(iso)}
    </time>
  );
}
