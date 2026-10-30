export function validateProxyOrigin(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const url = new URL(value);
  const loopback = ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname);
  if (!loopback || url.protocol !== "http:" || url.username || url.password || url.search || url.hash || (url.pathname !== "/" && url.pathname !== "")) {
    throw new Error("ROUTER_FORGE_API_URL must be an HTTP loopback origin without credentials");
  }
  return url.origin;
}
