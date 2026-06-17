import { describe, it, expect } from "vitest";
import { MENUS } from "@/config/menu";

describe("menu config", () => {
  it("has 13 first-level menus", () => {
    expect(MENUS).toHaveLength(13);
  });
  it("first menu is dashboard 安全概览", () => {
    expect(MENUS[0]).toMatchObject({ key: "dashboard", path: "/dashboard", title: "安全概览" });
  });
  it("includes alert-center 告警中心", () => {
    expect(MENUS.some((m) => m.key === "alert-center" && m.title === "告警中心")).toBe(true);
  });
  it("every menu has unique path", () => {
    const paths = MENUS.map((m) => m.path);
    expect(new Set(paths).size).toBe(paths.length);
  });
});
