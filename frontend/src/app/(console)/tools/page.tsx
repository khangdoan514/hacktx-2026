import PageHeader from "@/components/shell/PageHeader";
import ToolRegistry from "@/components/tools/ToolRegistry";

export default function ToolsPage() {
  return (
    <>
      <PageHeader
        title="Tools"
        sub="Register a draft tool and publish a version. Endpoints must be IP addresses, and a published version stays pinned for runs already in flight."
      />
      <ToolRegistry />
    </>
  );
}
