/**
 * Returns url if it is an absolute https: link, else undefined. Links that
 * come from API data go through this so a bad value can never become a
 * `javascript:` or other scheme link.
 */
export function safeHref(url: string | undefined | null): string | undefined {
  if (!url) return undefined;
  try {
    return new URL(url).protocol === "https:" ? url : undefined;
  } catch {
    return undefined;
  }
}
