#!/usr/bin/env -S node --experimental-strip-types --disable-warning=ExperimentalWarning
/**
 * UserPromptSubmit hook: read a message, analyse it, hand both back to the session.
 *
 * Claude Code never lets a hook replace what was typed, so this does not rewrite the
 * message. It gathers the facts around it (working directory, repository, branch, ticket,
 * the skills that exist, the rule headlines), asks a small model for a short restatement,
 * and prints the message and that restatement together as context.
 *
 * It fails open. A missing model, a timeout, an empty answer, a broken config file: each
 * one ends with the hook printing nothing and exiting 0, so a message always gets through.
 *
 * The child runs with no settings sources, which is what stops it triggering this hook
 * again. CLAUDE_PROMPT_ANALYSER is a second guard on the same thing.
 */
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  buildSystemPrompt,
  buildUserMessage,
  clip,
  collectSkills,
  formatAnalysis,
  loadConfig,
  renderHookOutput,
  renderLastRun,
  renderNotice,
  ruleIndex,
  shouldSkip,
  type AnalysisFacts,
  type FileSystemReader,
  type PromptAnalyserConfig,
  type RunOutcome,
} from "../lib/analyser.ts";

const HOME = homedir();
const HERE = dirname(fileURLToPath(import.meta.url));
const CONFIG_FILE = join(HERE, "..", "hook.config.json");
const GUARD = "CLAUDE_PROMPT_ANALYSER";
/** With this set, the hook reports what it sent, what came back, and how long it took. */
const DEBUG = process.env.CLAUDE_PROMPT_ANALYSER_DEBUG === "1";

function trace(label: string, detail: string): void {
  if (DEBUG) process.stderr.write(`[prompt-analyser] ${label}: ${detail}\n`);
}

const fs: FileSystemReader = {
  listEntries: (directory) => readdirSync(directory),
  readText: (file) => readFileSync(file, "utf8"),
};

const STARTED_AT = Date.now();

/**
 * Ends the run, recording how it went before exiting. Every path through the hook comes
 * through here, so the last run file always describes the most recent message.
 */
function finish(
  outcome: RunOutcome,
  config: PromptAnalyserConfig | null,
  prompt: string,
  context = "",
): never {
  if (config !== null && config.lastRunFile !== "") {
    try {
      writeFileSync(
        config.lastRunFile,
        renderLastRun(new Date(), outcome, Date.now() - STARTED_AT, prompt),
      );
    } catch {
      // a hook that cannot write its own log still has a job to do
    }
  }
  if (context !== "") process.stdout.write(context);
  process.exit(0);
}

function readFile(path: string): string {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return "";
  }
}

/** The org and repo of the checkout the session sits in, as `owner/name`. */
function repoOf(cwd: string): string {
  const url = git(cwd, ["config", "--get", "remote.origin.url"]);
  const match = /[:/]([^/:]+)\/([^/]+?)(?:\.git)?$/.exec(url);
  if (match === null) return "";
  return `${match[1]}/${match[2]}`;
}

function git(cwd: string, args: string[]): string {
  try {
    return execFileSync("git", ["-C", cwd, ...args], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
      timeout: 2_000,
    }).trim();
  } catch {
    return "";
  }
}

/** A ticket key such as GUN-1620 or VP-1531, taken from the branch or the path. */
function ticketOf(branch: string, cwd: string): string {
  const pattern = /\b([A-Z]{2,5}-\d+)\b/;
  const fromBranch = pattern.exec(branch.toUpperCase());
  if (fromBranch !== null) return fromBranch[1] ?? "";
  const fromPath = pattern.exec(cwd.toUpperCase());
  return fromPath?.[1] ?? "";
}

/** The fenced state block of a ticket's STATE.md, which says where the work stands. */
function ticketStateOf(ticket: string): string {
  if (ticket === "") return "";
  const orgsRoot = join(HOME, "claude", "orgs");
  let orgs: string[] = [];
  try {
    orgs = readdirSync(orgsRoot);
  } catch {
    return "";
  }
  for (const org of orgs) {
    const path = join(orgsRoot, org, "tickets", ticket, "STATE.md");
    if (!existsSync(path)) continue;
    const block = /```state\n([\s\S]*?)```/.exec(readFile(path));
    if (block !== null) return (block[1] ?? "").trim();
  }
  return "";
}

function gatherFacts(cwd: string): AnalysisFacts {
  const branch = git(cwd, ["rev-parse", "--abbrev-ref", "HEAD"]);
  const ticket = ticketOf(branch, cwd);
  return {
    cwd,
    repo: repoOf(cwd),
    branch,
    ticket,
    ticketState: ticketStateOf(ticket),
  };
}

/**
 * The environment the child runs in: the parent's, minus the variables the running
 * session set for itself, plus the guard and the thinking budget.
 */
function childEnv(): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {};
  for (const [key, value] of Object.entries(process.env)) {
    // CLAUDE_CONFIG_DIR is where the credentials live, so it has to survive
    if (key !== "CLAUDE_CONFIG_DIR" && (key.startsWith("CLAUDE_") || key === "CLAUDECODE")) {
      continue;
    }
    env[key] = value;
  }
  env[GUARD] = "1";
  env.MAX_THINKING_TOKENS = "0";
  return env;
}

/**
 * Runs the analysis on a child Claude with no settings, no tools and no session on disk.
 *
 * MAX_THINKING_TOKENS=0 is what makes this usable at all. With extended thinking left on,
 * the same call spent 3,855 thinking tokens and 42 seconds to produce five short lines.
 * With it off the call is about 1.5 seconds of interface time.
 */
function askForAnalysis(
  config: PromptAnalyserConfig,
  systemPrompt: string,
  userMessage: string,
): string {
  const startedAt = Date.now();
  const result = spawnSync(
    "claude",
    [
      "--print",
      "--model", config.model,
      "--system-prompt", systemPrompt,
      "--tools", "",
      "--setting-sources", "",
      "--strict-mcp-config",
      "--disable-slash-commands",
      "--no-session-persistence",
      "--output-format", "text",
    ],
    {
      input: userMessage,
      encoding: "utf8",
      timeout: config.timeoutMs,
      killSignal: "SIGKILL",
      cwd: HOME,
      env: childEnv(),
      stdio: ["pipe", "pipe", DEBUG ? "inherit" : "ignore"],
    },
  );
  trace("model", `${Date.now() - startedAt}ms, status ${String(result.status)}`);
  if (result.error !== undefined) trace("error", result.error.message);
  if (result.status !== 0 || typeof result.stdout !== "string") return "";
  trace("answer", result.stdout.trim());
  return result.stdout;
}

function main(): void {
  // the child model call must never analyse its own prompt
  if (process.env[GUARD] === "1") process.exit(0);

  let payload: { prompt?: unknown; cwd?: unknown };
  try {
    payload = JSON.parse(readFileSync(0, "utf8")) as typeof payload;
  } catch {
    process.exit(0);
  }

  const prompt = typeof payload.prompt === "string" ? payload.prompt : "";
  const cwd = typeof payload.cwd === "string" && payload.cwd !== "" ? payload.cwd : process.cwd();

  let parsedConfig: unknown = {};
  try {
    parsedConfig = JSON.parse(readFile(CONFIG_FILE) || "{}");
  } catch {
    parsedConfig = {};
  }
  const config = loadConfig(parsedConfig, HOME);

  if (shouldSkip(prompt, config)) finish("skipped", config, prompt);

  const userMessage = buildUserMessage({
    prompt,
    facts: gatherFacts(cwd),
    skills: collectSkills(config.skillDirs, fs),
    rules: config.rulesFile === "" ? [] : ruleIndex(readFile(config.rulesFile)),
    docs: config.docs
      .map((path) => ({
        label: path.split("/").pop()?.replace(/\.[^.]+$/, "") ?? "document",
        text: clip(readFile(path), config.maxDocChars),
      }))
      .filter((doc) => doc.text !== ""),
  });

  trace("sent", `${userMessage.length} characters`);

  const answer = askForAnalysis(config, buildSystemPrompt(), userMessage);
  if (answer.trim() === "") {
    finish("no answer", config, prompt, renderNotice("no answer, carrying on"));
  }

  const analysis = formatAnalysis(answer, config.maxAnalysisChars);
  if (analysis === null) {
    finish("pass", config, prompt, renderNotice("nothing to add"));
  }

  finish("analysed", config, prompt, renderHookOutput(prompt, analysis));
}

main();
