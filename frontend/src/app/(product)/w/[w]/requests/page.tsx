import { SimpleCreate } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <SimpleCreate
      title="Requests"
      path={`/workspaces/${w}/requests`}
      field="title"
      placeholder="Request title"
    />
  );
}
