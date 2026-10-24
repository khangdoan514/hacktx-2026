import { EntityScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; id: string }> }) {
  const { id } = await params;
  return <EntityScreen title="Request" path={`/requests/${id}`} />;
}
