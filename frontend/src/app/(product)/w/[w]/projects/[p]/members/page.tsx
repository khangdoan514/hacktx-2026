import { RecordList } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string }> }) {
  const { p } = await params;
  return (
    <RecordList
      title="Project members"
      path={`/projects/${p}/members`}
      empty="Nothing here yet."
      labelKey="display_name"
    />
  );
}
