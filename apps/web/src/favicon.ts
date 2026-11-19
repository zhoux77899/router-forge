import lightFaviconUrl from "../../../assets/favicon-light.svg?url";
import darkFaviconUrl from "../../../assets/favicon-dark.svg?url";
import { subscribeSystemTheme } from "@router-forge/ui/lib/appearance";

export { lightFaviconUrl, darkFaviconUrl };

export function subscribeSystemFavicon(media: MediaQueryList, link: Pick<HTMLLinkElement, "href">): () => void {
  const update = (dark: boolean) => { link.href = dark ? darkFaviconUrl : lightFaviconUrl; };
  update(media.matches);
  return subscribeSystemTheme(media, update);
}

export function initializeSystemFavicon(): () => void {
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (!link) return () => {};
  const media = window.matchMedia?.("(prefers-color-scheme: dark)");
  if (!media) { link.href = lightFaviconUrl; return () => {}; }
  return subscribeSystemFavicon(media, link);
}
