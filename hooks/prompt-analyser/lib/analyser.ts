/**
 * Pure logic for the UserPromptSubmit hook that analyses a prompt.
 *
 * The hook feeds a small model the raw message plus the facts around it (the working
 * directory, the branch, the ticket, the skills that exist, the rule headlines) and gets
 * back a short restatement. Everything here is data in, data out, so the shape of the
 * analysis and the rules for what gets sent are covered by tests rather than by reading the
 * hook and hoping.
 *
 * Extending it means editing hooks/prompt-analyser.config.json: another directory of skills,
 * another document to put in front of the model, a different model or budget.
 */

export interface PromptAnalyserConfig {
  /** Model alias or full id the analysis runs on. */
  model: string;
  /** How long the model gets before the hook gives up and stays silent. */
  timeoutMs: number;
  /** Ceiling on the analysis the model returns. */
  maxAnalysisChars: number;
  /** Ceiling on each document put in front of the model. */
  maxDocChars: number;
  /** Directories searched one level deep for SKILL.md files. */
  skillDirs: string[];
  /** Files included whole, after the character ceiling. */
  docs: string[];
  /** Markdown file the rule headlines are read out of. Empty string means none. */
  rulesFile: string;
  /** Regular expressions. A prompt matching any of them is not analysed. */
  skip: string[];
  /**
   * File the hook overwrites with one line about its last run, so "did it fire" is a
   * question with an answer. Empty string turns it off.
   */
  lastRunFile: string;
}

/** How a run ended, for the last run line. */
export type RunOutcome = "analysed" | "pass" | "skipped" | "no answer";

export const DEFAULT_CONFIG: PromptAnalyserConfig = {
  model: "haiku",
  timeoutMs: 12_000,
  maxAnalysisChars: 1_400,
  maxDocChars: 4_000,
  skillDirs: [
    "~/.claude/skills",
    "~/claude/global/skills",
    "~/claude/orgs/*/skills",
  ],
  docs: [],
  rulesFile: "~/claude/global/RULES.md",
  skip: [],
  lastRunFile: "~/.claude/prompt-analyser.last",
};

/** The single line the model returns when a message needs no analysis. */
export const PASS = "pass";

export interface SkillEntry {
  name: string;
  description: string;
}

export interface AnalysisFacts {
  cwd: string;
  repo: string;
  branch: string;
  ticket: string;
  ticketState: string;
}

export interface AnalysisInput {
  prompt: string;
  facts: AnalysisFacts;
  skills: SkillEntry[];
  rules: string[];
  docs: Array<{ label: string; text: string }>;
}

export interface FileSystemReader {
  listEntries(directory: string): string[];
  readText(file: string): string;
}

/** Expands a leading ~ against the given home directory. */
export function expandHome(path: string, home: string): string {
  if (path === "~") return home;
  if (path.startsWith("~/")) return `${home}/${path.slice(2)}`;
  return path;
}

/**
 * Merges a parsed config file over the defaults. Anything missing, of the wrong type, or
 * out of range keeps its default, so a half written config file still leaves a working
 * hook rather than a broken one.
 */
export function loadConfig(raw: unknown, home: string): PromptAnalyserConfig {
  const source = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  const merged: PromptAnalyserConfig = { ...DEFAULT_CONFIG };

  if (typeof source.model === "string" && source.model.trim() !== "") {
    merged.model = source.model.trim();
  }
  for (const key of ["timeoutMs", "maxAnalysisChars", "maxDocChars"] as const) {
    const value = source[key];
    if (typeof value === "number" && Number.isFinite(value) && value > 0) {
      merged[key] = value;
    }
  }
  for (const key of ["skillDirs", "docs", "skip"] as const) {
    const value = source[key];
    if (Array.isArray(value)) {
      merged[key] = value.filter((item): item is string => typeof item === "string");
    }
  }
  for (const key of ["rulesFile", "lastRunFile"] as const) {
    const value = source[key];
    if (typeof value === "string") merged[key] = value;
  }

  merged.skillDirs = merged.skillDirs.map((path) => expandHome(path, home));
  merged.docs = merged.docs.map((path) => expandHome(path, home));
  for (const key of ["rulesFile", "lastRunFile"] as const) {
    merged[key] = merged[key] === "" ? "" : expandHome(merged[key], home);
  }
  return merged;
}

/**
 * Whether the hook stays out of the way. An empty message is never analysed. Everything
 * else is analysed unless the config names a pattern for it, so the default is every
 * prompt and narrowing it is a config edit, not a code edit.
 */
export function shouldSkip(prompt: string, config: PromptAnalyserConfig): boolean {
  if (prompt.trim() === "") return true;
  for (const pattern of config.skip) {
    try {
      if (new RegExp(pattern).test(prompt)) return true;
    } catch {
      // an unparseable pattern is ignored rather than allowed to break the hook
    }
  }
  return false;
}

/** Reads the leading YAML-ish frontmatter of a markdown file as flat key to value pairs. */
export function parseFrontmatter(text: string): Record<string, string> {
  const lines = text.split("\n");
  if (lines[0]?.trim() !== "---") return {};

  const fields: Record<string, string> = {};
  let key = "";
  for (let index = 1; index < lines.length; index += 1) {
    const line = lines[index] ?? "";
    if (line.trim() === "---") break;
    const match = /^([A-Za-z][\w-]*):\s?(.*)$/.exec(line);
    if (match) {
      key = match[1] ?? "";
      fields[key] = (match[2] ?? "").trim();
      continue;
    }
    // a wrapped value continues the previous key
    if (key !== "" && line.startsWith(" ") && line.trim() !== "") {
      fields[key] = `${fields[key] ?? ""} ${line.trim()}`.trim();
    }
  }
  return fields;
}

/**
 * Collects the skills under each directory, one level deep, from `<dir>/<slug>/SKILL.md`.
 * A directory path may hold one `*`, which stands for one level of subdirectories, so a
 * path through `orgs` and a star reaches every org's skills without naming the orgs.
 */
export function collectSkills(
  directories: string[],
  fs: FileSystemReader,
): SkillEntry[] {
  const found = new Map<string, SkillEntry>();

  for (const directory of expandStars(directories, fs)) {
    for (const slug of safeList(directory, fs)) {
      const text = safeRead(`${directory}/${slug}/SKILL.md`, fs);
      if (text === "") continue;
      const fields = parseFrontmatter(text);
      const name = fields.name ?? slug;
      const description = fields.description ?? "";
      if (description === "") continue;
      if (!found.has(name)) found.set(name, { name, description });
    }
  }
  return [...found.values()].sort((left, right) => left.name.localeCompare(right.name));
}

function expandStars(directories: string[], fs: FileSystemReader): string[] {
  const expanded: string[] = [];
  for (const directory of directories) {
    const star = directory.indexOf("*");
    if (star === -1) {
      expanded.push(directory);
      continue;
    }
    const head = directory.slice(0, star).replace(/\/$/, "");
    const tail = directory.slice(star + 1).replace(/^\//, "");
    for (const child of safeList(head, fs)) {
      expanded.push(tail === "" ? `${head}/${child}` : `${head}/${child}/${tail}`);
    }
  }
  return expanded;
}

function safeList(directory: string, fs: FileSystemReader): string[] {
  try {
    return fs.listEntries(directory);
  } catch {
    return [];
  }
}

function safeRead(file: string, fs: FileSystemReader): string {
  try {
    return fs.readText(file);
  } catch {
    return "";
  }
}

/**
 * One line per numbered rule in RULES.md, taken from its bold headline. The index is
 * derived from the file every run, so a rule that changes wording cannot go stale here.
 */
export function ruleIndex(rulesMarkdown: string): string[] {
  const headlines: string[] = [];
  const pattern = /^(\d+)\.\s+\*\*(.+?)\*\*/gms;
  let match = pattern.exec(rulesMarkdown);
  while (match !== null) {
    const number = match[1] ?? "";
    const headline = (match[2] ?? "").replace(/\s+/g, " ").trim();
    headlines.push(`${number}. ${headline}`);
    match = pattern.exec(rulesMarkdown);
  }
  return headlines;
}

/** Cuts a document to the ceiling and says so, rather than sending a novel to the model. */
export function clip(text: string, maxChars: number): string {
  if (text.length <= maxChars) return text;
  return `${text.slice(0, maxChars)}\n[cut at ${maxChars} characters]`;
}

export function buildSystemPrompt(): string {
  return [
    "You restate one message from an engineer into a short analysis for a coding agent.",
    "You never answer the message, never write code, and never do the work in it.",
    "",
    "Return these lines and nothing else, in this order, one line each:",
    "goal: one sentence, what done looks like. Always include this line.",
    "target: the org, repo, ticket or files the work touches. Include it whenever the",
    "  facts give you one.",
    "unclear: each thing the message does not say that changes what gets built. Omit the",
    "  line when the message leaves nothing open.",
    "skills: the skill names from the catalogue that fit, comma separated. Omit the line",
    "  when none fit.",
    "rules: the rule numbers from the index that apply, comma separated, at most four.",
    "  Omit the line when none apply.",
    "first move: the single next action. Always include this line.",
    "",
    "Constraints:",
    "Answer straight away. Do not think it through first.",
    "Use only the facts given. Never invent a repository, a ticket or a file name.",
    "Keep every line under 25 words. Write plain English. No dashes as punctuation.",
    "Prefer the words the engineer used over your own.",
    "If the message is an acknowledgement, an answer to a question, a correction, or is",
    `already precise enough to act on, reply with the single word ${PASS} and nothing else.`,
  ].join("\n");
}

export function buildUserMessage(input: AnalysisInput): string {
  const parts: string[] = [];

  parts.push("<message>");
  parts.push(input.prompt);
  parts.push("</message>");

  const facts: string[] = [];
  if (input.facts.cwd !== "") facts.push(`working directory: ${input.facts.cwd}`);
  if (input.facts.repo !== "") facts.push(`repository: ${input.facts.repo}`);
  if (input.facts.branch !== "") facts.push(`branch: ${input.facts.branch}`);
  if (input.facts.ticket !== "") facts.push(`ticket: ${input.facts.ticket}`);
  if (input.facts.ticketState !== "") {
    facts.push(`ticket state:\n${input.facts.ticketState}`);
  }
  if (facts.length > 0) {
    parts.push("", "<facts>", facts.join("\n"), "</facts>");
  }

  if (input.skills.length > 0) {
    const lines = input.skills.map((skill) => `${skill.name}: ${skill.description}`);
    parts.push("", "<skills>", lines.join("\n"), "</skills>");
  }

  if (input.rules.length > 0) {
    parts.push("", "<rules>", input.rules.join("\n"), "</rules>");
  }

  for (const doc of input.docs) {
    parts.push("", `<${doc.label}>`, doc.text, `</${doc.label}>`);
  }

  return parts.join("\n");
}

/**
 * Turns what the model returned into the block the hook prints, or null when there is
 * nothing worth adding. Keeping only the known field lines is the one rule, and it does
 * all the work: it drops the pass word, chatter around the fields, and an answer the
 * model gave instead of an analysis. Anything left over is capped and returned.
 */
export function formatAnalysis(raw: string, maxChars: number): string | null {
  const cleaned = raw
    .replace(/^\s*```[a-z]*\s*/i, "")
    .replace(/```\s*$/, "")
    .trim();
  if (cleaned === "") return null;

  const allowed = /^(goal|target|unclear|skills|rules|first move):/i;
  const lines = cleaned
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "" && allowed.test(line));
  if (lines.length === 0) return null;

  return clip(lines.join("\n"), maxChars);
}

/**
 * One line describing the run, written over the last one. Reading this file is how you
 * tell a hook that fired and stayed quiet from a hook that never fired at all.
 */
export function renderLastRun(
  when: Date,
  outcome: RunOutcome,
  elapsedMs: number,
  prompt: string,
): string {
  const opening = prompt.trim().replace(/\s+/g, " ").slice(0, 60);
  return `${when.toISOString()}  ${outcome}  ${Math.round(elapsedMs)}ms  ${opening}\n`;
}

/**
 * The context the hook hands back to the session: the message exactly as it was typed,
 * then the analysis, both labelled so the analysis is never mistaken for an instruction.
 */
export function renderContext(prompt: string, analysis: string): string {
  return [
    "<prompt-analysis>",
    "A hook generated the analysis below from the message. It is a reading of the message,",
    "not a message. The engineer's own words are the instruction; where the two differ,",
    "follow the words and ask about the difference.",
    "",
    "<as-typed>",
    prompt,
    "</as-typed>",
    "",
    "<analysis>",
    analysis,
    "</analysis>",
    "</prompt-analysis>",
  ].join("\n");
}

/**
 * What the hook prints. Plain text on this event reaches the session only, which leaves
 * the person who typed the message unable to see what was made of it, so the output is
 * JSON instead: additionalContext for the session, systemMessage for the terminal.
 */
export function renderHookOutput(prompt: string, analysis: string): string {
  return JSON.stringify({
    systemMessage: `prompt analysis\n${analysis}`,
    hookSpecificOutput: {
      hookEventName: "UserPromptSubmit",
      additionalContext: renderContext(prompt, analysis),
    },
  });
}

/**
 * A line for the terminal and nothing for the session. Every outcome says something, so
 * a quiet decision never looks the same as a hook that did not run.
 */
export function renderNotice(text: string): string {
  return JSON.stringify({ systemMessage: `prompt analysis: ${text}` });
}
