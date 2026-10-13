import { useEffect, useRef, useState } from "react";
import type { T } from "../api";
import { useApp } from "../context";
import { sendID } from "../optimistic.mjs";
import { Button } from "../ui/Button";

/** The host still validates the current owner key and exact live callback.
 * Delivery of a response says nothing about the native operation's completion. */
export function NativeRequest({ n, report, result }: { n: T.NativeRequest; report: string; result?: T.DecisionResult }) {
  const store = useApp();
  const [busy, setBusy] = useState(false), [sent, setSent] = useState(false);
  const [expired, setExpired] = useState(n.expires > 0 && Date.now() >= n.expires);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [values, setValues] = useState<Record<string, string | boolean>>({});
  const prepared = useRef<{ choice: string; text: string; id: string } | null>(null);
  const sending = useRef(false);
  useEffect(() => {
    if (n.expires === 0) { setExpired(false); return; }
    let timer: ReturnType<typeof setTimeout>;
    const update = () => {
      const remaining = n.expires - Date.now(); setExpired(remaining <= 0);
      if (remaining > 0) timer = setTimeout(update, Math.min(remaining, 2147483647));
    };
    update();
    return () => clearTimeout(timer);
  }, [n.token, n.expires]);
  useEffect(() => { if (result?.refused) { prepared.current = null; setSent(false); } }, [result?.decision, result?.refused]);
  const decided = !!result && !result.refused;
  const disabled = busy || sent || expired || decided;
  const formReady = (n.questions || []).every(q => !!answers[q.id]?.trim()) && (n.fields || []).every(f => !f.required || Object.hasOwn(values, f.id));
  const submit = async (choice: string) => {
    if (disabled || sending.current || choice === "submit" && !formReady) return;
    if (prepared.current && prepared.current.choice !== choice) return;
    prepared.current ||= { choice, id: sendID(), text: "agentnet-native-response-v1\n" + JSON.stringify({ token: n.token, choice, ...(choice === "submit" && n.questions?.length ? { answers } : {}), ...(choice === "submit" && n.fields?.length ? { values } : {}) }) };
    const p = prepared.current;
    sending.current = true; setBusy(true);
    try {
      const local = store.host.platform !== "browser" && n.host === store.host.workspace.address;
      const saved = local
        ? await store.run(api => api.act({ do: "continue", id: n.request, key: n.key, attempt: n.attempt, body: p.text, send_id: p.id }))
        : await store.run(api => api.decide({ host: n.host, id: n.request, key: n.key, attempt: n.attempt, action: "continue", expect: "running", text: p.text, report, send_id: p.id }));
      if (saved) { setSent(true); setAnswers({}); setValues({}); store.toast("Response saved for the same native operation. Waiting for the host’s result."); }
    } finally { sending.current = false; setBusy(false); }
  };
  return <section aria-label="Codex native request" className="space-y-3 rounded-xl border border-approval-ink/30 bg-surface p-3">
    <p className="font-bold">Codex native request</p>
    <p className="text-[14px] text-text-2">Your Codex setup asks for this permission or input. Your response continues this same pending operation, with your native settings unchanged.</p>
    {n.cwd && <p className="text-[13px] [overflow-wrap:anywhere]">Working folder: {n.cwd}</p>}
    {n.operation && <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-lg bg-sunken p-2 text-[13px] [overflow-wrap:anywhere]">{n.operation}</pre>}
    {n.reason && <p className="whitespace-pre-wrap text-[14px] [overflow-wrap:anywhere]">{n.reason}</p>}
    {n.unsupported && <p role="status" className="text-[14px] [overflow-wrap:anywhere]">{n.unsupported}</p>}
    {(n.questions || []).map(q => <label key={q.id} className="block space-y-1 text-[14px]">
      <span className="block font-semibold">{q.question}</span>
      {q.options?.length && !q.isOther && !q.isSecret ? <select aria-label={q.question} value={answers[q.id] || ""} disabled={disabled || !!prepared.current} className="w-full rounded-lg border border-hairline bg-surface p-2" onChange={e => setAnswers({ ...answers, [q.id]: e.target.value })}>
        <option value="" disabled>Choose an answer</option>{q.options.map(o => <option key={o.label} value={o.label}>{o.label}{o.description ? " — " + o.description : ""}</option>)}
      </select> : <input type={q.isSecret ? "password" : "text"} autoComplete="off" aria-label={q.question} value={answers[q.id] || ""} disabled={disabled || !!prepared.current} className="w-full rounded-lg border border-hairline bg-surface p-2" onChange={e => setAnswers({ ...answers, [q.id]: e.target.value })} />}
      {q.isOther && !!q.options?.length && <span className="block text-text-2">Choices: {q.options.map(o => o.label).join(", ")}. You can also write an answer.</span>}
    </label>)}
    {(n.fields || []).map(f => <label key={f.id} className="block space-y-1 text-[14px]">
      <span className="block font-semibold">{f.title || f.id}{f.required ? " (required)" : ""}</span>
      {f.description && <span className="block text-text-2">{f.description}</span>}
      {f.type === "boolean" || f.enum?.length ? <select aria-label={f.title || f.id} value={Object.hasOwn(values, f.id) ? String(values[f.id]) : ""} disabled={disabled || !!prepared.current} className="w-full rounded-lg border border-hairline bg-surface p-2" onChange={e => setValues({ ...values, [f.id]: f.type === "boolean" ? e.target.value === "true" : e.target.value })}>
        <option value="" disabled>Choose a value</option>{(f.type === "boolean" ? ["true", "false"] : f.enum || []).map(v => <option key={v} value={v}>{v}</option>)}
      </select> : <input aria-label={f.title || f.id} value={String(values[f.id] || "")} disabled={disabled || !!prepared.current} className="w-full rounded-lg border border-hairline bg-surface p-2" onChange={e => setValues({ ...values, [f.id]: e.target.value })} />}
    </label>)}
    <div className="flex flex-wrap gap-2">{(n.choices || []).map(choice => <Button key={choice} size="sm" variant={choice === "allow" || choice === "submit" ? "act" : "outline"} disabled={disabled || !!prepared.current && prepared.current.choice !== choice || choice === "submit" && !formReady} onClick={() => void submit(choice)}>
      {busy ? "Sending…" : prepared.current?.choice === choice && !sent ? "Retry response" : ({ allow: "Allow this action", deny: "Deny this action", cancel: "Cancel", submit: "Send native answer" } as Record<string, string>)[choice] || choice}
    </Button>)}</div>
    {(expired || sent || decided) && <p role="status" className="text-[13px]">{expired ? "This native request has expired. No response can be applied to it." : "Response saved. The host will report the native result; saved does not mean completed."}</p>}
    {result?.refused && <p role="alert" className="text-[13px] text-danger">Not applied: {result.refused}</p>}
  </section>;
}
