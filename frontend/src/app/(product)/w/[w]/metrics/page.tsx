import { SimpleCreate } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return (
    <SimpleCreate
      title="Metrics"
      path={`/workspaces/${w}/metrics`}
      field="name"
      placeholder="Metric name"
    />
  );
}
