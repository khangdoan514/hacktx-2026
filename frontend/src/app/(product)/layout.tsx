import ProductShell from "@/product/shell";

export default function ProductLayout({ children }: { children: React.ReactNode }) {
  return <ProductShell>{children}</ProductShell>;
}
