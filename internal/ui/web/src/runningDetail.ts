// Presentation only: this exact clause is produced by client/runvisibility.go.
// A report's exec.at can be a snapshot time, so it must not replace this start.
export function localRunningDetail(detail: string, running: boolean): string {
  if (!running) return detail;
  const match=/^((?:Seems stuck: no harness output or progress for \d+ minutes\. )?Running since )(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z)(\. Stop is available; nothing stops automatically unless you set a time limit\.)$/.exec(detail);
  if (!match) return detail;
  const date=new Date(match[2]);
  if (!Number.isFinite(date.getTime()) || date.toISOString() !== match[2].replace("Z",".000Z")) return detail;
  return match[1]+date.toLocaleString([], {year:"numeric",month:"short",day:"numeric",hour:"numeric",minute:"2-digit"})+match[3];
}
