import type { Fetch } from "../api";

export type FakeCall = { path: string; method: string; body: unknown };

export type FakeRoute = { status: number; body?: unknown; raw?: string };

export class FakeServer {
  readonly calls: FakeCall[] = [];
  private routes = new Map<string, FakeRoute | Error>();

  on(method: string, path: string, route: FakeRoute | Error): this {
    this.routes.set(method + " " + path, route);
    return this;
  }

  readonly fetch: Fetch = async (input, init) => {
    const method = init?.method ?? "GET";
    const body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    this.calls.push({ path: input, method, body });
    const route = this.routes.get(method + " " + input);
    if (route instanceof Error) {
      throw route;
    }
    if (!route) {
      return new Response(JSON.stringify({ error: { code: "not_found", message: "no route" } }), { status: 404 });
    }
    const text = route.raw ?? (route.body === undefined ? "" : JSON.stringify(route.body));
    return new Response(text, { status: route.status, headers: { "Content-Type": "application/json" } });
  };
}

export class MemoryStorage {
  private values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }
}
