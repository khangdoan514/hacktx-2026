import PageHeader from "@/components/shell/PageHeader";
import ToolPacks from "@/components/tools/ToolPacks";
import ToolRegistry from "@/components/tools/ToolRegistry";

export default function ToolsPage() {
  return (
    <>
      <PageHeader
        title="Tools"
        sub="Connect built-in tools, or register a draft tool and publish a version. Endpoints must be IP addresses, and a published version stays pinned for runs already in flight."
      />
      <ToolPacks />
      <ToolRegistry />
    </>
  );
}
