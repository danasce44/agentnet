// The frame around a request that needs someone (skin/README.md): a question
// or task waiting for your OK, running, stopped, or handed to a person. The
// card's content (who asks, what, the actions) comes from the interface as
// children and must stay reachable: a skin may hide it behind one explicit
// "open" step, never drop or reword it. tone: act (your decision), work
// (running), quiet (stopped or handed over). label names the state; at is
// when the request arrived.
import type { ReactNode } from "react";
import { timeOf } from "../model";

const bands = {
  act: "bg-act text-act-ink dots",
  work: "bg-agent text-agent-ink dots",
  quiet: "bg-sunken text-text-2",
};

export function RequestFrame({ tone, label, at, children, labelledBy }: { tone: keyof typeof bands; label: string; at: string; children: ReactNode; labelledBy?: string }) {
  return (
    <article aria-labelledby={labelledBy} data-part="request" data-tone={tone} className={"fade-in w-full overflow-hidden rounded-2xl bg-surface stroke " + (tone === "quiet" ? "" : "shadow-pop")}>
      <header className={"flex items-center justify-between gap-3 border-b-[1.5px] border-outline px-3.5 py-2 " + bands[tone]}>
        <span className={"inline-flex h-6 -rotate-2 items-center gap-1.5 rounded-md stroke px-2 text-[11px] font-extrabold uppercase tracking-[0.07em] " + (tone === "act" ? "bg-surface text-ink" : "bg-surface")}>
          {tone === "work" && <span className="flex gap-0.5" aria-hidden="true"><i className="working-dot size-1 rounded-full bg-current" /><i className="working-dot size-1 rounded-full bg-current" /><i className="working-dot size-1 rounded-full bg-current" /></span>}
          {label}
        </span>
        <time dateTime={at} className="tnum text-[13px] font-bold">{timeOf(at)}</time>
      </header>
      {children}
    </article>
  );
}
