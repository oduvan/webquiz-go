// Quiz text and student input are plain text. React escapes text children, so these
// helpers only turn values into strings. Never use dangerouslySetInnerHTML for them.
export function plain(value: unknown): string {
  return value === null || value === undefined ? '' : String(value);
}
