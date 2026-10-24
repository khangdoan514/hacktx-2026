import { MyWorkScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string }> }) {
  const { w } = await params;
  return <MyWorkScreen workspaceId={w} />;
}
