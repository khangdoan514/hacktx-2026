import { ProjectScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string }> }) {
  const { w, p } = await params;
  return <ProjectScreen workspaceId={w} projectId={p} />;
}
