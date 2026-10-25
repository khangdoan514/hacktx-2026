import s from "./Logo.module.css";

/** Wordmark that follows the console theme, or one fixed version. */
export default function Logo({ className, theme }: { className?: string; theme?: "light" | "dark" }) {
  if (theme) {
    return <img className={className} src={`/brand/nmc3-${theme}.png`} alt="NMC³" />;
  }
  return (
    <span className={`${s.logo} ${className ?? ""}`}>
      <img className={s.light} src="/brand/nmc3-light.png" alt="NMC³" />
      <img className={s.dark} src="/brand/nmc3-dark.png" alt="NMC³" />
    </span>
  );
}
