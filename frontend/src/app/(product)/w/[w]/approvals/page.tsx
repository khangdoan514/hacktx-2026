import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <RecordList
      title="Approvals"
      path={`/workspaces/${w}/approvals`}
      empty="Nothing here yet."
      labelKey="target_type"
    />
  );
}
