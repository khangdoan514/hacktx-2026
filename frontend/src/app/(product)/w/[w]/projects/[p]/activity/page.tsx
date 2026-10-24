import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string }> }) {
  const { w, p } = await params;
  return (
    <RecordList
      title="Activity"
      path={`/projects/${p}/activity?workspace_id=${w}`}
      empty="No activity yet."
      labelKey="action"
    />
  );
}
