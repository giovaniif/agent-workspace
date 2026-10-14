import type { KeyValue } from "./auth";

export const legacyAuthKey = "agentws.auth";
export const legacyChoicesKey = "agentws.new";
export const legacyCachePrefix = "agentws-shell-";
export const legacyBrowserWord = "agentws";

export function adoptLegacy(storage: KeyValue, key: string, legacy: string): string | null {
  const current = storage.getItem(key);
  if (current) {
    return current;
  }
  const old = storage.getItem(legacy);
  if (old) {
    storage.setItem(key, old);
  }
  return old;
}
