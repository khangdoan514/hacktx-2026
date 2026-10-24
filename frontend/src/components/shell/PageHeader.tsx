import CodeHint, { type Snippets } from "@/components/ui/CodeHint";
import s from "./PageHeader.module.css";

export default function PageHeader({
  title, sub, code, actions, crumbs,
}: {
  title: React.ReactNode;
  sub?: React.ReactNode;
  code?: Snippets;
  actions?: React.ReactNode;
  crumbs?: React.ReactNode;
}) {
  return (
    <header className={`${s.head} rise`}>
      <div className={s.l}>
        {crumbs && <div className={s.crumbs}>{crumbs}</div>}
        <h1>{title}</h1>
        {sub && <p className={s.sub}>{sub}</p>}
      </div>
      <div className={s.r}>
        {code && <CodeHint code={code} />}
        {actions}
      </div>
    </header>
  );
}
