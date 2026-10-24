import { EntityScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string; id: string }> }) {
  const { id } = await params;
  return <EntityScreen title="Handoff" path={`/handoffs/${id}`} />;
}
