import { useTranslation } from "react-i18next";
import { parseNodeProjects } from "@/lib/nodeProjects";
import { cn } from "@/lib/utils";

interface NodeProjectsProps {
  publicRemark?: string | null;
  compact?: boolean;
  maxVisible?: number;
  showLabel?: boolean;
  className?: string;
}

/**
 * Renders deployed projects parsed from a node's public_remark.
 * In compact mode, enforces a fixed single-line height so collapsed cards remain uniform.
 */
export function NodeProjects({
  publicRemark,
  compact = false,
  maxVisible = 2,
  showLabel = false,
  className,
}: NodeProjectsProps) {
  const { t } = useTranslation();
  const projects = parseNodeProjects(publicRemark);

  if (compact) {
    const visible = projects.slice(0, maxVisible);
    const hiddenCount = projects.length - visible.length;
    const fullTitle = projects.join(", ");

    return (
      <div
        className={cn("flex h-6 min-w-0 items-center gap-1 overflow-hidden", className)}
        title={projects.length > 0 ? fullTitle : undefined}
        aria-label={projects.length > 0 ? `${t("nodeCard.projects")}: ${fullTitle}` : t("nodeCard.noProjects")}
      >
        {projects.length === 0 ? (
          <span className="truncate text-[11px] text-muted-foreground/60">
            {t("nodeCard.noProjects")}
          </span>
        ) : (
          <>
            {visible.map((project) => (
              <span
                key={project}
                className="inline-flex h-5 max-w-[7.5rem] shrink items-center truncate rounded-md border border-primary/20 bg-primary/10 px-1.5 text-[10px] font-medium leading-none text-primary"
              >
                {project}
              </span>
            ))}
            {hiddenCount > 0 && (
              <span className="inline-flex h-5 shrink-0 items-center rounded-md border border-border bg-muted/60 px-1.5 text-[10px] font-medium leading-none text-muted-foreground">
                +{hiddenCount}
              </span>
            )}
          </>
        )}
      </div>
    );
  }

  if (projects.length === 0) return null;

  return (
    <div className={cn("space-y-1.5", className)}>
      {showLabel && (
        <div className="text-xs font-medium text-muted-foreground">
          {t("nodeCard.projects")}
        </div>
      )}
      <div className="flex flex-wrap gap-1.5">
        {projects.map((project) => (
          <span
            key={project}
            className="inline-flex items-center rounded-md border border-primary/20 bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary"
          >
            {project}
          </span>
        ))}
      </div>
    </div>
  );
}
