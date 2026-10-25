"use client";

import Link from "next/link";
import { useEffect, useRef } from "react";
import { mountNetwork } from "./network";
import s from "./Landing.module.css";

export default function Landing() {
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    if (canvas.current) return mountNetwork(canvas.current);
  }, []);

  return (
    <div className={s.landing}>
      <main className={s.main}>
        <section className={s.intro} aria-labelledby="landing-title">
          <div className={s.content}>
            <h1 id="landing-title"><img className={s.wordmark} src="/brand/nmc3-dark.png" alt="NMC³" /></h1>
            <p className={s.slogan}>Cloud, Compute,<br />Consolidate.</p>
            <p className={s.description}>A shared context for your agents,<br />your teams, and the work between them.</p>
            <Link className={s.explore} href="/runs">Explore <span aria-hidden="true">↗</span></Link>
          </div>
          <p className={s.note}>One framework. Collective intelligence.</p>
        </section>
        <section className={s.network} aria-label="Interactive flowing agent network">
          <canvas ref={canvas} tabIndex={0} role="img" aria-label="Agent network. Click a dot to highlight its connections. Arrow keys select agents, Space pauses movement, and Escape clears selection." />
          <div className={s.hint}>Move to explore · Click an agent</div>
        </section>
      </main>
      <footer className={s.footer}><span>NMC³</span><span>Cloud / Compute / Consolidate</span></footer>
    </div>
  );
}
