import { afterEach, describe, expect, test } from "bun:test";
import { APIClient, APIError, type NetworkProfileInput } from "./index";

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

const input: NetworkProfileInput = {
  profile: {
    id: "", revision: "", name: "Explicit model network", scope: "desktop",
    proxy_mode: "proxy", proxy_url: "https://proxy.invalid:8443", proxy_targets: ["https"],
    proxy_auth: "basic", relay_mode: "loopback", tls_mode: "verify", trust_mode: "system",
    has_credentials: false, tls_risk_accepted: false, created_at: "",
  },
  proxy_username: "fixture-private-user", proxy_password: "fixture-private-password",
  confirm_tls_risk: false,
};

const saved = { ...input.profile, id: "net_1", revision: "network_revision_1", has_credentials: true, credential_revision: "credential_revision_1" };

describe("native network profile contract", () => {
  test("uses authenticated native transport for public metadata and write-only credentials", async () => {
    globalThis.fetch = Object.assign(async () => { throw new Error("native settings must not use browser fetch"); }, originalFetch);
    const calls: { method: string; path: string; body: string }[] = [];
    const client = new APIClient("", "", async (method, path, body) => {
      calls.push({ method, path, body });
      return { status: 200, body: JSON.stringify(method === "GET" ? [saved] : saved) };
    });
    const result = await client.saveNetworkProfile(input);
    expect(result).toEqual(saved);
    expect(await client.networkProfiles()).toEqual([saved]);
    expect(calls[0]).toEqual({ method: "POST", path: "/api/network-profiles", body: JSON.stringify(input) });
    expect(calls[1]).toEqual({ method: "GET", path: "/api/network-profiles", body: "" });
    expect(result).not.toHaveProperty("proxy_username");
    expect(result).not.toHaveProperty("proxy_password");
    expect(input.proxy_password).toBe("fixture-private-password");
  });

  test("binds checks to an explicit revision and destination without inferring risk approval", async () => {
    const calls: { method: string; path: string; body: string }[] = [];
    const observation = { profile_id: saved.id, profile_revision: saved.revision, target_url: "https://target.invalid", reachable: false, issues: ["unavailable"], observed_at: "2026-01-01T00:00:00Z" };
    const client = new APIClient("", "", async (method, path, body) => {
      calls.push({ method, path, body });
      return { status: 200, body: JSON.stringify(observation) };
    });
    const checkInput = { profile_revision: saved.revision, target_url: observation.target_url, confirm_tls_risk: false };
    expect(await client.checkNetworkProfile(saved.id, checkInput)).toEqual(observation);
    expect(calls).toEqual([{ method: "POST", path: "/api/network-profiles/net_1/check", body: JSON.stringify(checkInput) }]);
    expect(calls[0]?.body).not.toContain(input.proxy_password!);
    expect(JSON.parse(calls[0]?.body ?? "{}")).not.toHaveProperty("confirm_paid");
  });

  test("stale revisions fail once without a browser or network-policy fallback", async () => {
    globalThis.fetch = Object.assign(async () => { throw new Error("must not fall back to fetch"); }, originalFetch);
    let calls = 0;
    const client = new APIClient("", "", async () => { calls++; return { status: 409, body: JSON.stringify({ error: "network profile revision changed" }) }; });
    try {
      await client.checkNetworkProfile(saved.id, { profile_revision: "old_revision", target_url: "https://target.invalid" });
      throw new Error("expected revision conflict");
    } catch (error) {
      expect(error).toBeInstanceOf(APIError);
      expect((error as APIError).status).toBe(409);
    }
    expect(calls).toBe(1);
  });
});
