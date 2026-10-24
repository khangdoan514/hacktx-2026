import { EntityScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; id: string }> }) {
  const { w } = await params;
  return <EntityScreen title="Question" path={`/workspaces/${w}/questions`} />;
}
