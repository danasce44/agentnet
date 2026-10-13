// What a skin shows above a topic's messages (skin/README.md). Comic shows
// nothing: its topic bar already names the topic. A skin may draw a title
// card from these facts; it must not hide or delay the messages under it.
//   title  the topic's name ("Main" for a chat's main flow)
//   main   true for the main flow of a people chat (no topic chosen)
//   state  active, done or archived
//   index  its 1-based place in this chat's topic list, when the view has one
//   at     when its first shown message arrived (ISO time), if any
//   count  how many messages it shows
export interface TopicFacts { title: string; main: boolean; state: "active" | "done" | "archived"; index?: number; at?: string; count: number }

export function TopicIntro(_: TopicFacts) {
  return null;
}
