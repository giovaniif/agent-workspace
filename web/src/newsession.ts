import type { KeyValue } from "./auth";

export type Choices = {
  workspace: string;
  harness: string;
  model: string;
  effort: string;
};

const key = "agentws.new";

export const harnesses = [
  { id: "claude", label: "Claude" },
  { id: "codex", label: "Codex" },
];

export const modelChoices: Record<string, string[]> = {
  claude: ["opus", "sonnet", "haiku"],
  codex: ["gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.5"],
};

export const effortChoices = ["low", "medium", "high", "xhigh", "max"];

export function defaultLabel(value: string | undefined): string {
  const name = (value ?? "").trim();
  return name ? "default (" + name + ")" : "default";
}

export function loadChoices(storage: KeyValue): Choices {
  const fallback: Choices = { workspace: "", harness: "claude", model: "", effort: "" };
  try {
    const raw = storage.getItem(key);
    if (!raw) {
      return fallback;
    }
    const parsed = JSON.parse(raw) as Partial<Choices>;
    const text = (v: unknown) => (typeof v === "string" ? v : "");
    return {
      workspace: text(parsed.workspace),
      harness: harnesses.some((h) => h.id === parsed.harness) ? (parsed.harness as string) : fallback.harness,
      model: text(parsed.model),
      effort: effortChoices.includes(text(parsed.effort)) ? text(parsed.effort) : "",
    };
  } catch {
    return fallback;
  }
}

export function saveChoices(storage: KeyValue, choices: Choices): void {
  try {
    storage.setItem(key, JSON.stringify(choices));
  } catch {
    return;
  }
}
