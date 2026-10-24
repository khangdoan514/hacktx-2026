import { TaskScreen } from "@/product/screens";

export default async function Page({ params }: { params: Promise<{ w: string; p: string; task: string }> }) {
  const { w, p, task } = await params;
  return <TaskScreen workspaceId={w} projectId={p} taskId={task} />;
}
