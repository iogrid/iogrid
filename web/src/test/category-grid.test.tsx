import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { CategoryGrid } from "@/components/dashboard/category-grid";
import { CATEGORIES } from "@/lib/categories";

describe("CategoryGrid", () => {
  it("renders one checkbox per category", () => {
    render(
      <CategoryGrid
        categories={CATEGORIES}
        selected={[]}
        onToggle={() => {}}
      />,
    );
    expect(screen.getAllByRole("checkbox")).toHaveLength(CATEGORIES.length);
  });

  it("toggles a category through onToggle", () => {
    const onToggle = vi.fn();
    render(
      <CategoryGrid
        categories={CATEGORIES.slice(0, 1)}
        selected={[]}
        onToggle={onToggle}
      />,
    );
    fireEvent.click(screen.getByRole("checkbox"));
    expect(onToggle).toHaveBeenCalledWith(CATEGORIES[0].slug, true);
  });

  it("marks selected categories as checked", () => {
    render(
      <CategoryGrid
        categories={CATEGORIES.slice(0, 2)}
        selected={[CATEGORIES[1].slug]}
        onToggle={() => {}}
      />,
    );
    const boxes = screen.getAllByRole("checkbox");
    expect((boxes[0] as HTMLInputElement).checked).toBe(false);
    expect((boxes[1] as HTMLInputElement).checked).toBe(true);
  });
});
