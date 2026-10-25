import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import ToolPacks from "@/components/tools/ToolPacks";
import ToolRegistry from "@/components/tools/ToolRegistry";
import s from "./tools.module.css";

export default function ToolsPage() {
  return (
    <>
      <PageHeader
        title="Tools"
        sub="Connect your accounts to give workspace agents access to your documents, repositories, Notion pages, and Slack conversations."
      />
      <ToolPacks />
      <details className={s.custom}>
        <summary>
          <div><b>Build a custom tool</b><span>For integrations that are not in the directory</span></div>
          <Icon name="plus" size={18} />
        </summary>
        <div className={s.customBody}><ToolRegistry /></div>
      </details>
    </>
  );
}
