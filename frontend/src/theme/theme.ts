import type { ThemeConfig } from "antd";

export const theme: ThemeConfig = {
  token: {
    colorPrimary: "#4f46e5",
    colorInfo: "#4f46e5",
    colorBgLayout: "#f6f7f9",
    colorBorderSecondary: "#eceef2",
    colorTextBase: "#111827",
    borderRadius: 10,
    borderRadiusLG: 12,
    fontFamily:
      "var(--font-geist-sans), -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
    fontSize: 14,
    boxShadowTertiary: "0 1px 2px rgba(16, 24, 40, 0.04)",
  },
  components: {
    Layout: {
      siderBg: "#ffffff",
      headerBg: "#ffffff",
      headerHeight: 60,
      headerPadding: "0 24px",
    },
    Menu: {
      itemBg: "transparent",
      itemSelectedBg: "#eef2ff",
      itemSelectedColor: "#4338ca",
      itemBorderRadius: 8,
      itemMarginInline: 8,
    },
    Card: {
      paddingLG: 20,
    },
    Table: {
      headerBg: "#fafbfc",
      headerColor: "#6b7280",
    },
  },
};
