import { WorkspaceNav } from "@/product/shell";

export default async function WorkspaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ w: string }>;
}) {
  const { w } = await params;
  return (
    <>
      <WorkspaceNav workspaceId={w} />
      {children}
    </>
  );
}
