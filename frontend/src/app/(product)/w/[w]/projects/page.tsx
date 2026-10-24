import { ProjectsScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return <ProjectsScreen workspaceId={w} />;
}
