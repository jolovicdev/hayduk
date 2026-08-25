const PROMPT_RE = /(?:^|\n)(msf\d*(?: [^\r\n]*)? >[ \t]*)$/;

export interface ConsolePrompt {
  value: string;
  start: number;
}

export function parsePrompt(output: string): ConsolePrompt | null {
  const match = PROMPT_RE.exec(output);
  if (!match) return null;
  const leadingNewline = match[0].startsWith("\n") ? 1 : 0;
  return { value: match[1]!, start: match.index + leadingNewline };
}
