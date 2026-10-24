import { SimpleCreate } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <SimpleCreate
      title="Partners"
      path={`/workspaces/${w}/partners`}
      field="name"
      placeholder="Partner organization"
    />
  );
}
