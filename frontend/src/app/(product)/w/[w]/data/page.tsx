import { SimpleCreate } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <SimpleCreate
      title="Datasets"
      path={`/workspaces/${w}/datasets`}
      field="name"
      placeholder="Dataset name"
    />
  );
}
