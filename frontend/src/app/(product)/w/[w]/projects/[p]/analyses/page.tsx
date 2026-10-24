import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string }> }) {
  const { p } = await params;
  return (
    <RecordList
      title="Analyses"
      path={`/projects/${p}/analyses`}
      empty="Nothing here yet."
      labelKey="title"
    />
  );
}
