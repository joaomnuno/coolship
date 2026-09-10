/**
 * A small tokenizer for the landing page's code samples. The samples are a
 * handful of fixed strings in five languages, so a few regular expressions
 * per language cover them without pulling a highlighter into the page bundle
 * or the request path. Each token carries a `tk-*` class from global.css.
 */
export type Lang = "bash" | "json" | "toml" | "yaml" | "text";

export interface Token {
  text: string;
  cls?: string;
}

interface Spec {
  /** Alternatives in priority order; each capture group maps to `classes[i]`. */
  pattern: RegExp;
  classes: string[];
}

const commands = "coolship|coolify|curl|sh|cd|npm|go|echo|export|source|git";

const specs: Record<Lang, Spec> = {
  bash: {
    pattern: new RegExp(
      [
        "(#.*$)",
        "(\"(?:[^\"\\\\]|\\\\.)*\"|'[^']*')",
        "(https?://[^\\s\"']+)",
        "(?<![\\w-])(--?[A-Za-z][\\w-]*)",
        "(^\\$)(?= )",
        `(?<![\\w./-])(${commands})(?![\\w-])`,
        "(\\||&&)",
      ].join("|"),
      "g",
    ),
    classes: [
      "tk-cmt",
      "tk-str",
      "tk-url",
      "tk-flag",
      "tk-prompt",
      "tk-cmd",
      "tk-punc",
    ],
  },
  json: {
    pattern:
      /("(?:[^"\\]|\\.)*")(?=\s*:)|("(?:[^"\\]|\\.)*")|(-?\d+(?:\.\d+)?)|\b(true|false|null)\b|([{}[\],:])/g,
    classes: ["tk-key", "tk-str", "tk-num", "tk-num", "tk-punc"],
  },
  toml: {
    pattern:
      /(#.*$)|(^\s*\[[^\]]+\])|(^\s*[A-Za-z_][\w.-]*)(?=\s*=)|("(?:[^"\\]|\\.)*"|'[^']*')|\b(\d+)\b/g,
    classes: ["tk-cmt", "tk-sec", "tk-key", "tk-str", "tk-num"],
  },
  yaml: {
    pattern: new RegExp(
      [
        "(#.*$)",
        "(^\\s*(?:-\\s+)?[A-Za-z_][\\w.-]*)(?=:(?:\\s|$))",
        "(\\$\\{\\{[^}]*\\}\\})",
        "(\"[^\"]*\"|'[^']*')",
        "(https?://\\S+)",
        `(?<![\\w./-])(${commands})(?![\\w-])`,
        "(?<![\\w-])(--?[A-Za-z][\\w-]*)",
      ].join("|"),
      "g",
    ),
    classes: [
      "tk-cmt",
      "tk-key",
      "tk-num",
      "tk-str",
      "tk-url",
      "tk-cmd",
      "tk-flag",
    ],
  },
  // Terminal transcripts: a prompt line, doctor's status tags, and the words
  // that mark a good outcome.
  text: {
    pattern:
      /(^\$)(?= )|(?<=^\$ )(.+)$|(\[ok\])|(\[warn\])|(\[fail\])|(https?:\/\/[^\s"']+)|(finished|running:healthy|"healthy")/g,
    classes: [
      "tk-prompt",
      "tk-cmd",
      "tk-ok",
      "tk-warn",
      "tk-err",
      "tk-url",
      "tk-ok",
    ],
  },
};

export function tokenizeLine(line: string, lang: Lang): Token[] {
  const spec = specs[lang];
  const out: Token[] = [];
  let last = 0;
  for (const match of line.matchAll(spec.pattern)) {
    const index = match.index;
    if (match[0].length === 0) continue;
    if (index > last) out.push({ text: line.slice(last, index) });
    const group = match.slice(1).findIndex((value) => value !== undefined);
    out.push({ text: match[0], cls: spec.classes[group] });
    last = index + match[0].length;
  }
  if (last < line.length) out.push({ text: line.slice(last) });
  return out;
}

export function tokenize(code: string, lang: Lang): Token[][] {
  return code
    .replace(/\n$/, "")
    .split("\n")
    .map((line) => tokenizeLine(line, lang));
}
