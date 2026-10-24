import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string }> }) {
  const { p } = await params;
  return (
    <RecordList
      title="Documents"
      path={`/projects/${p}/documents`}
      empty="Nothing here yet."
      labelKey="title"
    />
  );
}
