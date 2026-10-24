import { SimpleCreate } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string }> }) {
  const { p } = await params;
  return (
    <SimpleCreate
      title="Requirements"
      path={`/projects/${p}/requirements`}
      field="title"
      placeholder="Requirement"
    />
  );
}
