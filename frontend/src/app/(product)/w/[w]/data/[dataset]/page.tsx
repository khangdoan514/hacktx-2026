import { EntityScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; dataset: string }> }) {
  const { w } = await params;
  return <EntityScreen title="Dataset" path={`/workspaces/${w}/datasets`} />;
}
