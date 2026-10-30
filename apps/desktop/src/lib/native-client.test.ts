import { expect, test } from "bun:test";
import { connectDesktop } from "./native-client";

test("ordinary browser cannot fall back to control HTTP requests", async () => {
  Reflect.deleteProperty(globalThis, "mygo");
  await expect(connectDesktop()).rejects.toThrow("MyGo 原生桥接");
});

test("generated native client wiring keeps control credential out of renderer connection", async () => {
  const calls: { method: string; args: unknown[] }[] = [];
  Reflect.set(globalThis, "mygo", {
    call: async (method: string, ...args: unknown[]) => {
      calls.push({ method, args });
      return method === "Console.Connect" ? { url: "http://127.0.0.1:1234", pid: 12, version: "test", data_dir: "test-data", worker_dir: "test-workers" } : { status: 200, body: JSON.stringify({ running: true, pid: 12, version: "test" }) };
    },
  });
  try {
    const desktop = await connectDesktop();
    await desktop.api.status();
    expect(Object.keys(desktop.connection)).not.toContain("control_token");
    expect(calls.map(call => call.method)).toEqual(["Console.Connect", "Console.Request"]);
    expect(calls[1]?.args).toEqual(["GET", "/api/status", ""]);
  } finally { Reflect.deleteProperty(globalThis, "mygo"); }
});
