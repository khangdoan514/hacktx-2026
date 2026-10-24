import { EntityScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string; id: string }> }) {
  const { p } = await params;
  return <EntityScreen title="Document" path={`/projects/${p}/documents`} />;
}
