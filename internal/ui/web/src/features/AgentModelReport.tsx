// Private read-only runtime metadata, never an availability or settings claim.
import type { T } from "../api";
export function AgentModelReport({ overview, host, agentId = "" }: { overview: T.Overview; host: string; agentId?: string }) {
  const own = overview.person;
  const device = own?.state === "self" ? own.devices?.find(d => d.address === host) : undefined;
  if (!device) return null;
  const report = overview.model_reports?.find(r => r.host === host && r.host_key === device.fingerprint && r.agent_id === agentId);
  const date = report ? new Date(report.at * 1000) : null;
  const knownTime = date !== null && Number.isFinite(date.getTime());
  return <p className="pt-1 text-[13px] text-muted [overflow-wrap:anywhere]" data-agent-model={host + "#" + agentId}>
    {report ? <>
      <span className="block">Last reported model: {report.model} · Reasoning: {report.reasoning || "not reported"}</span>
      <span className="block"><time dateTime={knownTime ? date!.toISOString() : undefined}>{knownTime ? date!.toLocaleString() : "Timestamp unavailable"}</time> · Settings may have changed.</span>
    </> : "Model and reasoning not reported for this agent’s current setup. They appear after a run that reports its runtime."}
  </p>;
}
