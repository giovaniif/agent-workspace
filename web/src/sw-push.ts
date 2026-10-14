export type PushPayload = {
  title: string;
  body: string;
  url: string;
  tag: string;
};

export type PushDataLike = {
  json: () => unknown;
  text: () => string;
};

export type NotificationSpec = {
  title: string;
  options: {
    body: string;
    tag?: string;
    icon: string;
    badge: string;
    data: { url: string };
  };
};

export type WindowLike = {
  url: string;
  focus: () => Promise<unknown>;
  navigate?: (url: string) => Promise<unknown>;
};

export type ClientsLike = {
  matchAll: (options: { type: "window"; includeUncontrolled: boolean }) => Promise<readonly WindowLike[]>;
  openWindow: (url: string) => Promise<unknown>;
};

const fallbackTitle = "crew";
const fallbackBody = "needs you";

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function appURL(value: unknown): string {
  const url = text(value);
  return url.startsWith("/") && !url.startsWith("//") && !url.startsWith("/\\") ? url : "/";
}

export function payloadOf(data: PushDataLike | null | undefined): PushPayload {
  if (!data) {
    return { title: "", body: "", url: "/", tag: "" };
  }
  let parsed: unknown;
  try {
    parsed = data.json();
  } catch {
    return { title: "", body: text(data.text()), url: "/", tag: "" };
  }
  const record = typeof parsed === "object" && parsed !== null ? (parsed as Record<string, unknown>) : {};
  return { title: text(record.title), body: text(record.body), url: appURL(record.url), tag: text(record.tag) };
}

export function notificationFor(p: PushPayload): NotificationSpec {
  return {
    title: p.title || fallbackTitle,
    options: {
      body: p.body || fallbackBody,
      tag: p.tag || undefined,
      icon: "/icon-192.png",
      badge: "/icon-192.png",
      data: { url: appURL(p.url) },
    },
  };
}

export async function openFromNotification(clients: ClientsLike, origin: string, url: string): Promise<void> {
  const target = appURL(url);
  const windows = await clients.matchAll({ type: "window", includeUncontrolled: true });
  const open = windows.find((w) => {
    try {
      return new URL(w.url).origin === origin;
    } catch {
      return false;
    }
  });
  if (open?.navigate) {
    try {
      await open.focus();
      await open.navigate(new URL(target, origin).href);
      return;
    } catch {
      await clients.openWindow(target);
      return;
    }
  }
  await clients.openWindow(target);
}
