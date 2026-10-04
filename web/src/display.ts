export type DisplayWindow = {
  matchMedia?: (query: string) => { matches: boolean };
  navigator: { standalone?: boolean; userAgent?: string };
};

export function isInstalled(win: DisplayWindow): boolean {
  if (win.navigator.standalone === true) {
    return true;
  }
  return Boolean(win.matchMedia?.("(display-mode: standalone)").matches);
}

const codeAlphabet = /[^23456789ABCDEFGHJKMNPQRSTUVWXYZ]/g;

export function normalizePairCode(raw: string): string {
  return raw.toUpperCase().replace(codeAlphabet, "").slice(0, 8);
}

export function pairCodeFromHash(hash: string): string {
  const params = new URLSearchParams(hash.replace(/^#/, ""));
  return normalizePairCode(params.get("pair") ?? "");
}

export function formatPairCode(code: string): string {
  return code.length > 4 ? code.slice(0, 4) + " " + code.slice(4) : code;
}

export function defaultDeviceName(userAgent: string): string {
  if (/iPad/.test(userAgent)) {
    return "iPad";
  }
  if (/iPhone/.test(userAgent)) {
    return "iPhone";
  }
  if (/Android/.test(userAgent)) {
    return "Android phone";
  }
  return "phone";
}
