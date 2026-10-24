import { SimpleCreate } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <SimpleCreate
      title="Data questions"
      path={`/workspaces/${w}/questions`}
      field="question"
      placeholder="Question for the DBA"
    />
  );
}
