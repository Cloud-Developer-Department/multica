"use client";

// Status badge for the analytics platform. The shadcn `Badge` has no
// success/warning variants, so this thin wrapper maps a semantic status to
// token classes (no hard-coded colors). Variants:
//   success — green tint (completed / resolved / merged)
//   warning — amber tint (in progress / open / pending)
//   secondary — neutral (closed / draft / offboard)

import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";

export type StatusTone = "success" | "warning" | "secondary" | "destructive";

const TONE_CLASS: Record<StatusTone, string> = {
  success: "bg-success/10 text-success border-transparent",
  warning: "bg-warning/10 text-warning border-transparent",
  secondary: "bg-secondary text-secondary-foreground",
  destructive: "bg-destructive/10 text-destructive border-transparent",
};

export function StatusBadge({
  tone,
  className,
  ...props
}: React.ComponentProps<typeof Badge> & { tone: StatusTone }) {
  return (
    <Badge variant="secondary" className={cn(TONE_CLASS[tone], className)} {...props} />
  );
}
