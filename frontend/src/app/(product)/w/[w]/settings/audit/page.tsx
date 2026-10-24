import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <RecordList
      title="Audit"
      path={`/workspaces/${w}/audit`}
      empty="Nothing here yet."
      labelKey="action"
    />
  );
}
