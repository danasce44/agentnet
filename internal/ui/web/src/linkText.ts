import { Lexer, type Token, type Tokens } from "marked";
export { localRunningDetail } from "./runningDetail";

const entities: Record<string,string> = {amp:"&",lt:"<",gt:">",quot:'"',apos:"'",nbsp:" "};
export const markdownText = (s: string): string => s.replace(/&(#x[0-9a-f]{1,6}|#[0-9]{1,7}|[a-z]{2,6});/gi, (all, ref: string) => {
  if (ref[0] !== "#") return entities[ref.toLowerCase()] ?? all;
  const n = /^#x/i.test(ref) ? parseInt(ref.slice(2),16) : parseInt(ref.slice(1),10);
  return n > 0 && n <= 0x10ffff ? String.fromCodePoint(n) : all;
});
export const markdownTokens = (text: string): Token[] => new Lexer({gfm:true,breaks:true}).lex(text);

// Only previews use this projection. Literal code/escapes and raw HTML remain
// text; the message's signed body, copy target and user-chosen title stay intact.
export function previewText(text: string): string {
  if (!/[\\`*_~[\]<>!&#\n]|^\s*(?:[-+>]\s|\d+[.)]\s)/.test(text)) return text;
  const read = (tokens: Token[]): string => tokens.map(token => {
    const t = token as Tokens.Generic;
    if (t.type === "space" || t.type === "br") return "\n";
    if (t.type === "def" || t.type === "hr") return "";
    if (t.type === "list") return (t as Tokens.List).items.map(it => read(it.tokens)).join("\n") + "\n";
    if (t.type === "table") {
      const table=t as Tokens.Table;
      return [table.header,...table.rows].map(row=>row.map(c=>read(c.tokens)).join(" · ")).join("\n")+"\n";
    }
    const value=t.tokens ? read(t.tokens) : t.type === "text" ? markdownText(t.text) : t.text ?? t.raw;
    return value + (["paragraph","heading","blockquote","code"].includes(t.type) ? "\n" : "");
  }).join("");
  return read(markdownTokens(text));
}

// Presentation only. The original message and href remain the copy/open targets.
export function shortLinkLabel(label: string, href: string): string {
  if (label.length <= 64 || !/^https?:\/\//i.test(href)) return label;
  if (label !== href && `https://${label}` !== href && `http://${label}` !== href) return label;
  try {
    const url = new URL(href);
    // Include userinfo when present: never conceal a misleading authority.
    const authority = href.slice(href.indexOf("://") + 3).split(/[/?#]/, 1)[0];
    if (!url.hostname || authority.length > 60) return label;
    return authority + "/…";
  } catch { return label; }
}

export type LinkPart = string | { href: string; label: string };

// Plain-text skins (the Classic and Zoom packages in agentnet-skins) keep
// their formatting and take this as skins/shared/link-text.mjs (build.sh).
// Use the same Markdown lexer as Comic to recognize raw links without
// touching code or descriptive links.
export function rawLinkParts(text: string): LinkPart[] {
  if (text.length <= 64 || !/https?:\/\/|www\./i.test(text)) return [text];
  const links: { start: number; end: number; href: string; label: string }[] = [];
  function scan(source: string, tokens: Token[], offset: number) {
    let cursor = 0;
    for (const token of tokens) {
      const at = source.indexOf(token.raw, cursor);
      if (at < 0) return; // A normalized construct stays plain text.
      cursor = at + token.raw.length;
      const t = token as Tokens.Generic;
      if (t.type === "link") {
        const label = shortLinkLabel(t.text, t.href);
        if (label !== t.text) links.push({ start: offset + at, end: offset + cursor, href: t.href, label });
      } else if (!["code", "codespan", "html", "image"].includes(t.type)) {
        if (t.tokens) scan(t.raw, t.tokens, offset + at);
        else if (t.type === "list") scan(t.raw, (t as Tokens.List).items, offset + at);
      }
    }
  }
  scan(text, new Lexer({ gfm: true }).lex(text), 0);
  const parts: LinkPart[] = [];
  let cursor = 0;
  for (const link of links) {
    if (link.start < cursor) continue;
    parts.push(text.slice(cursor, link.start), { href: link.href, label: link.label });
    cursor = link.end;
  }
  parts.push(text.slice(cursor));
  return parts;
}
