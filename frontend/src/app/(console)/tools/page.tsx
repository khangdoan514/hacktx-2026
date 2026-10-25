import PageHeader from "@/components/shell/PageHeader";
import ToolPacks from "@/components/tools/ToolPacks";
import ToolRegistry from "@/components/tools/ToolRegistry";

export default function ToolsPage() {
  return (
    <>
      <PageHeader
        title="Tools"
        sub="Connect your accounts to give workspace agents access to your documents, repositories, and Notion pages."
      />
      <ToolPacks />
      <details style={{ marginTop: 24 }}>
        <summary style={{ cursor: "pointer", marginBottom: 16 }}>Advanced: custom tools</summary>
        <ToolRegistry />
      </details>
    </>
  );
}
