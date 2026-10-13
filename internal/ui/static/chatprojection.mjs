// Saved-row queries and presentation only. Admission and actions retain their
// own current reads and atomic write guards in the engine.
import { deviceHistoryDirection, deviceHistoryPeer } from './devicehistory.mjs';

export function threadPeer(row, store) {
  if (!row || row.conv || !(row.v === 1 || row.control)) return '';
  return deviceHistoryPeer(row, store === 'inbox' ? row.from : row.to) || '';
}

export function threadIndexed(row, store) {
  const value = { ...row }, peer = threadPeer(row, store);
  if (peer) value.thread_peer = peer;
  else delete value.thread_peer;
  return value;
}

export async function readThreadRows(store, id) {
  const [incoming, outgoing] = await Promise.all([store.get('inbox', id), store.get('outbox', id)]);
  const peer = threadPeer(incoming, 'inbox') || threadPeer(outgoing, 'outbox');
  if (!peer) return { inbox: [], outbox: [] };
  const [inbox, outbox] = await Promise.all([store.byThreadPeer('inbox', peer), store.byThreadPeer('outbox', peer)]);
  return { inbox, outbox };
}

export function projectDeviceThreads({ inbox, outbox }, { address, fingerprint, erased }) {
  const msgs = [
    ...inbox.filter(m => m.v === 1 && !m.aside && !erased(m)).map(m => ({ ...m, dir: deviceHistoryDirection(m, 'in'), peer: threadPeer(m, 'inbox'), author_fp: m.fp, stored_in: true })),
    ...outbox.filter(m => m.v === 1 && !m.aside && !erased(m)).map(m => ({ ...m, dir: deviceHistoryDirection(m, 'out', m.from || address), peer: threadPeer(m, 'outbox'), author_fp: fingerprint })),
  ];
  const byID = new Map(msgs.map(m => [m.id, m])), parent = new Map(msgs.map(m => [m.id, m.id]));
  const find = x => {
    while (parent.get(x) !== x) { parent.set(x, parent.get(parent.get(x))); x = parent.get(x); }
    return x;
  };
  for (const m of msgs) {
    const reply = m.reply_to && byID.get(m.reply_to);
    if (reply && reply.peer === m.peer) parent.set(find(m.id), find(reply.id));
  }
  const groups = new Map();
  // Self-delivery retains both transport copies. Present the sent row once,
  // as the native client does; its ID also reconciles optimistic send previews.
  for (const m of byID.values()) {
    const root = find(m.id);
    let group = groups.get(root);
    if (!group) groups.set(root, group = []);
    group.push(m);
  }
  return [...groups.values()].map(g => g.sort((a, b) => a.at - b.at || (a.id < b.id ? -1 : 1)));
}

export async function readChatView(store, { me, fingerprint }, conv = null) {
  const [incoming, outgoing, persons, convs, groups, reports, decisions] = await Promise.all([
    conv === null ? store.all('inbox') : store.byConv('inbox', conv),
    conv === null ? store.all('outbox') : store.byConv('outbox', conv),
    store.all('persons'), store.all('convs'), store.prefix('kv', 'group/'),
    conv === null ? [] : store.inboxStatus('review_notice'),
    conv === null ? [] : store.outboxSub('decision'),
  ]);
  const unique = rows => [...new Map(rows.map(r => [r.id, r])).values()];
  const inbox = reports.length ? unique([...incoming, ...reports]) : incoming;
  const outbox = decisions.length ? unique([...outgoing, ...decisions]) : outgoing;
  const byConv = { inbox: new Map(), outbox: new Map() }, fpPersons = new Map();
  for (const [name, rows] of [['inbox', inbox], ['outbox', outbox]]) for (const row of rows) {
    let selected = byConv[name].get(row.conv);
    if (!selected) byConv[name].set(row.conv, selected = []);
    selected.push(row);
  }
  for (const person of [me, ...persons].filter(Boolean)) for (const device of person.known || person.devices || [])
    if (!fpPersons.has(device.fingerprint)) fpPersons.set(device.fingerprint, person.person);
  if (me) fpPersons.set(fingerprint, me.person);
  return { inbox, outbox, persons, convs, groups, byConv, fpPersons, parts: new Map() };
}
