import { Lexer, type Tokens } from "marked";

// Display only: client/agentjob.go splitEmotion, assistantreaction.go
// splitReaction and trailers.go, limited to a terminal plain Markdown line.
// The stored original result and all execution/status fields stay untouched.
export function requestResultText(original: string): string {
  let text = original;
  const trimSpace = (s: string) => s.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
  const take = (kind: "emotion" | "reaction" | "topic"): boolean => {
    const end = text.replace(/[ \t\r\n]+$/, ""), at = end.lastIndexOf("\n");
    if (at < 0) return false;
    const body = trimSpace(end.slice(0, at)), line = end.slice(at + 1);
    if (!body) return false;
    const block = new Lexer({ gfm: true }).lex(end).at(-1) as Tokens.Generic | undefined;
    const tail = block?.tokens?.at(-1) as Tokens.Generic | undefined;
    if (block?.type !== "paragraph" || tail?.type !== "text" || !tail.raw.endsWith(line)) return false;
    const match = /^([^:]+):([\s\S]*)$/.exec(line);
    if (!match || trimSpace(match[1]).toLowerCase() !== kind) return false;
    const value = trimSpace(match[2]);
    if (kind === "emotion" ? !/^[a-z0-9-]{1,24}$/.test(value)
      : kind === "reaction" ? !oneEmoji(value.replace(/^-/, ""))
      : trimSpace(line).toLowerCase() !== "topic: done") return false;
    text = body;
    return true;
  };
  take("emotion");
  let reaction = false, topic = false;
  for (let n = 0; n < 2; n++) {
    if (!reaction && take("reaction")) reaction = true;
    else if (!topic && take("topic")) topic = true;
    else break;
  }
  return text;
}

// Exact browser counterparts of envelope.ValidEmoji/OneEmoji, from wire.mjs.
const MaxEmojiBytes = 64, MaxEmojiRunes = 12;
const utf8 = new TextEncoder();
function wellFormed(s: string) {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const d = s.charCodeAt(i + 1);
      if (!(d >= 0xdc00 && d <= 0xdfff)) return false;
      i++;
    } else if (c >= 0xdc00 && c <= 0xdfff) return false;
  }
  return true;
}
function validEmoji(s: string) {
  if (typeof s !== "string" || !s || utf8.encode(s).length > MaxEmojiBytes || !wellFormed(s) || [...s].length > MaxEmojiRunes) return false;
  let symbol = false;
  const keycap = s.includes("\u20E3");
  for (const ch of s) {
    const r = ch.codePointAt(0)!;
    if (r === 0x200D || r === 0xFE0F || r === 0xFE0E || r === 0x20E3 || (r >= 0xE0020 && r <= 0xE007F) || (r >= 0x1F3FB && r <= 0x1F3FF)) continue;
    if (keycap && ((r >= 0x30 && r <= 0x39) || ch === "#" || ch === "*")) { symbol = true; continue; }
    if ((r >= 0x1F1E6 && r <= 0x1F1FF) || r >= 0x1F000 || /\p{S}/u.test(ch)) { symbol = true; continue; }
    return false;
  }
  return symbol;
}

function oneEmoji(s: string) {
  if (!validEmoji(s)) return false;
  let bases = 0, joined = false, pairing = false;
  for (const ch of s) {
    const r = ch.codePointAt(0)!;
    if (r === 0x200D) { joined = true; continue; }
    if (r === 0xFE0F || r === 0xFE0E || r === 0x20E3 || (r >= 0xE0020 && r <= 0xE007F) || (r >= 0x1F3FB && r <= 0x1F3FF)) continue; // part of the emoji before it
    if (r >= 0x1F1E6 && r <= 0x1F1FF) { // regional indicators: two make one flag
      if (pairing) { pairing = false; continue; }
      pairing = true;
    } else if ((r >= 0x30 && r <= 0x39) || ch === "#" || ch === "*" || emojiBase(r, ch)) pairing = false;
    else return false;
    if (!joined) bases++;
    joined = false;
  }
  return bases === 1 && !joined;
}

// emojiBase is envelope.emojiBase: a rune of the emoji blocks, an arrow,
// an emoji that is a math symbol, or another symbol of the Basic
// Multilingual Plane but the braille blank, which shows nothing.
function emojiBase(r: number, ch: string) {
  if (r >= 0x1F000 && r <= 0x1FAFF) return true;
  if (r > 0xFFFF || r === 0x2800) return false;
  if ((r >= 0x2190 && r <= 0x21FF) || (r >= 0x25FB && r <= 0x25FE) || r === 0x2934 || r === 0x2935) return true;
  return /\p{So}/u.test(ch);
}

