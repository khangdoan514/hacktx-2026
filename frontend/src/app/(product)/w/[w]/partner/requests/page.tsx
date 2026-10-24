import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <RecordList
      title="Your requests"
      path={`/workspaces/${w}/requests`}
      empty="Nothing here yet."
      labelKey="title"
    />
  );
}
