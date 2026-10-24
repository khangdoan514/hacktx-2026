import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <RecordList
      title="Members"
      path={`/workspaces/${w}/members`}
      empty="Nothing here yet."
      labelKey="display_name"
    />
  );
}
