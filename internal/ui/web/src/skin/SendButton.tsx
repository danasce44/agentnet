// The composer's send control (skin/README.md). It must stay a submit button
// named by `label` (Send, Send your reply…), refuse while not ready or busy
// (aria-disabled, so the field keeps focus), and keep focus in the field on
// a tap (onMouseDown={keep}). An overlay skin may draw it however it likes.
import { IconArrowUp } from "@tabler/icons-react";
import type { MouseEvent as ReactMouseEvent } from "react";

/** keep stops a tap from taking focus off the text field. */
const keep = (e: ReactMouseEvent) => e.preventDefault();

export function SendButton({ ready, busy, label }: { ready: boolean; busy: boolean; label: string }) {
  const look = ready ? "bg-act stroke shadow-pop-sm press hover:brightness-[1.03]" : "bg-sunken border-[1.5px] border-hairline lg:border";
  const ink = ready ? "text-act-ink" : "text-muted";
  const dots = <span aria-hidden="true" className={"flex gap-0.5 text-lg leading-none " + ink}><span className="working-dot">•</span><span className="working-dot">•</span><span className="working-dot">•</span></span>;
  return (
    <button type="submit" data-part="send" onMouseDown={keep} aria-disabled={!ready || busy} aria-label={label} title={label}
      className={"grid size-[46px] shrink-0 place-items-center self-end rounded-full transition-colors duration-200 lg:size-12 " + look}>
      {busy ? dots : <IconArrowUp size={24} stroke={2.6} aria-hidden="true" className={ink} />}
    </button>
  );
}
