import type { Device, Paired } from "./api";

export type KeyValue = Pick<Storage, "getItem" | "setItem" | "removeItem">;

export type Auth = {
  token: string;
  device: Device;
};

const authKey = "crew.auth";
const legacyAuthKey = "agentws.auth";

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

export function loadAuth(storage: KeyValue): Auth | null {
  try {
    const raw = adoptLegacy(storage, authKey, legacyAuthKey);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as Partial<Auth>;
    if (typeof parsed.token !== "string" || !parsed.token || typeof parsed.device !== "object" || !parsed.device) {
      return null;
    }
    return { token: parsed.token, device: parsed.device };
  } catch {
    return null;
  }
}

export function clearAuth(storage: KeyValue): void {
  storage.removeItem(authKey);
  storage.removeItem(legacyAuthKey);
}

export function saveAuth(storage: KeyValue, paired: Paired): Auth {
  const auth = { token: paired.token, device: paired.device };
  storage.setItem(authKey, JSON.stringify(auth));
  return auth;
}
