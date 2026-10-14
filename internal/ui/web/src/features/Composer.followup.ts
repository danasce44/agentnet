import type { T } from "../api";
import { followupRef } from "./Message.followup";
import type { Ctx } from "./Message.model";

export interface ComposerFollowup {
  ref: { id: string; fingerprint: string };
  kind: string;
}

// A display name or the last message never chooses work. The exact current
// PID, host key, selected topic and original human request must all match.
export function composerFollowup(m: T.DMMessage, pid: string, topic: string, ctx: Ctx): ComposerFollowup | null {
  const agent = ctx.dm?.agents?.find(a => a.pid === pid), ref = followupRef(m, ctx);
  if (!ref || !agent?.can_ask || m.pid !== pid || (m.topic || "") !== topic ||
      m.target?.address !== agent.host.address || m.target?.fingerprint !== agent.host.fingerprint ||
      (m.target?.agent_id || "") !== (agent.agent_id || "")) return null;
  return { ref, kind: m.kind };
}

export function activeComposerFollowups(dm: T.DMThread | undefined, pids: string[], topic: string, ctx: Ctx) {
  const result = new Map<string, T.DMMessage[]>(pids.map(pid => [pid, []]));
  for (const message of dm?.messages || []) {
    const rows = result.get(message.pid || "");
    if (!rows || !composerFollowup(message, message.pid!, topic, ctx)) continue;
    const state = message.exec?.state;
    if (state === "running" || state === "queued" || state === "part_waiting" || state === "pending" || state === "accepted") rows.push(message);
  }
  return result;
}
