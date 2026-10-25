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
        sub="Find existing integrations, connect the services your agents need, and manage access in one place."
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
