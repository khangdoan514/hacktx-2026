import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <RecordList
      title="Inbox"
      path={`/workspaces/${w}/notifications`}
      empty="Nothing here yet."
      labelKey="preview"
    />
  );
}
