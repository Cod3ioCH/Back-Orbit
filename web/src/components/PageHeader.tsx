import type { ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  description?: string;
  /** Primary actions, right-aligned on wider screens. */
  actions?: ReactNode;
}

/**
 * PageHeader keeps the title/description/action rhythm identical on every
 * page. Previously each page hand-rolled this and they had drifted apart in
 * spacing and in how actions were aligned.
 */
export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && (
          <p className="mt-1 text-sm text-muted-foreground">{description}</p>
        )}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}
