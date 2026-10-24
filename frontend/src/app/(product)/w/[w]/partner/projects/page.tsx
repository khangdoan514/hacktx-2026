import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <RecordList
      title="Shared projects"
      path={`/workspaces/${w}/partner/projects`}
      empty="Nothing here yet."
      labelKey="name"
    />
  );
}
