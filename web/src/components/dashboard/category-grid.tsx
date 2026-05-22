"use client";

import * as React from "react";
import { cn } from "@/lib/utils";
import type { Category } from "@/lib/categories";

/**
 * CategoryGrid — opt-in checklist of workload categories the provider
 * accepts. Used in the schedule editor + onboarding wizard.
 *
 * Selected state is owned by the parent so it can submit the diff via
 * the /provider/schedule endpoint.
 */
export interface CategoryGridProps {
  categories: Category[];
  selected: string[];
  onToggle: (slug: string, next: boolean) => void;
  disabled?: boolean;
}

export function CategoryGrid({
  categories,
  selected,
  onToggle,
  disabled = false,
}: CategoryGridProps) {
  const selSet = React.useMemo(() => new Set(selected), [selected]);
  return (
    <ul
      role="group"
      aria-label="Workload categories"
      className="grid grid-cols-1 gap-3 sm:grid-cols-2"
    >
      {categories.map((cat) => {
        const on = selSet.has(cat.slug);
        return (
          <li key={cat.slug}>
            <label
              className={cn(
                "flex cursor-pointer items-start gap-3 rounded-md border p-3 transition-colors",
                on
                  ? "border-success/40 bg-success/10 dark:bg-success/15"
                  : "border-border bg-card hover:border-foreground/40 dark:border-border",
                disabled && "cursor-not-allowed opacity-60",
              )}
            >
              <input
                type="checkbox"
                checked={on}
                disabled={disabled}
                onChange={(e) => onToggle(cat.slug, e.target.checked)}
                className="mt-0.5 h-4 w-4 accent-emerald-600"
                aria-describedby={`cat-desc-${cat.slug}`}
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="text-sm font-medium">{cat.label}</span>
                  <span className="text-xs text-muted-foreground">
                    {cat.customers} customers
                  </span>
                </div>
                <p
                  id={`cat-desc-${cat.slug}`}
                  className="mt-0.5 text-xs text-muted-foreground dark:text-muted-foreground"
                >
                  {cat.description}
                </p>
              </div>
            </label>
          </li>
        );
      })}
    </ul>
  );
}
