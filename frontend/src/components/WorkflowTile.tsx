import {
  BarChartOutlined,
  BellOutlined,
  CalendarOutlined,
  CodeOutlined,
  FileTextOutlined,
  MailOutlined,
  MessageOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import type { Workflow } from "@/lib/mock-data";

const icons: Record<Workflow["icon"], React.ReactNode> = {
  mail: <MailOutlined />,
  calendar: <CalendarOutlined />,
  file: <FileTextOutlined />,
  search: <SearchOutlined />,
  chart: <BarChartOutlined />,
  message: <MessageOutlined />,
  bell: <BellOutlined />,
  code: <CodeOutlined />,
};

// Apple Shortcuts-style tile: solid color, icon top-left, title bottom-left.
export default function WorkflowTile({ workflow }: { workflow: Workflow }) {
  return (
    <button
      type="button"
      className="flex h-32 w-full cursor-pointer flex-col justify-between rounded-2xl p-4 text-left text-white transition hover:-translate-y-0.5 hover:shadow-lg"
      style={{
        background: `linear-gradient(145deg, ${workflow.color}, color-mix(in srgb, ${workflow.color} 75%, black))`,
      }}
    >
      <span className="text-xl">{icons[workflow.icon]}</span>
      <span>
        <span className="block text-[15px] font-semibold leading-tight">{workflow.name}</span>
        <span className="text-xs opacity-80">{workflow.steps} steps</span>
      </span>
    </button>
  );
}
