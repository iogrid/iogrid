// Jest mock for `expo-linking`.
//
// expo-linking ships native-bridge code (TurboModules + iOS-only URL
// scheme registration) that cannot load under node. The mock here
// implements the subset of the API our wallet adapters touch:
//   - openURL(url)             → captures the launched URL
//   - canOpenURL(url)          → toggled per-test via __setCanOpen()
//   - addEventListener(name,h) → registers a callback for tests to
//                                drive a synthetic `url` event
//   - parse(url)               → minimal URL → {scheme, queryParams}
//
// Tests reach into the helpers via the named exports prefixed `__`,
// which are excluded from the production typings.

type UrlListener = (event: { url: string }) => void;

let canOpenMap = new Map<string, boolean>();
let canOpenDefault = true;
const openHistory: string[] = [];
const urlListeners: UrlListener[] = [];

export async function openURL(url: string): Promise<true> {
  openHistory.push(url);
  return true;
}

export async function canOpenURL(url: string): Promise<boolean> {
  if (canOpenMap.has(url)) return canOpenMap.get(url)!;
  // Also try scheme prefix lookup ("phantom://").
  const m = url.match(/^[^:]+:\/\//);
  if (m && canOpenMap.has(m[0])) return canOpenMap.get(m[0])!;
  return canOpenDefault;
}

export function addEventListener(
  event: string,
  handler: UrlListener,
): { remove(): void } {
  if (event !== 'url') {
    return { remove() {} };
  }
  urlListeners.push(handler);
  return {
    remove() {
      const i = urlListeners.indexOf(handler);
      if (i >= 0) urlListeners.splice(i, 1);
    },
  };
}

export function parse(url: string): {
  scheme: string | null;
  hostname: string | null;
  path: string | null;
  queryParams: Record<string, string>;
} {
  let scheme: string | null = null;
  let rest = url;
  const schemeMatch = url.match(/^([^:]+):\/\/(.*)$/);
  if (schemeMatch) {
    scheme = schemeMatch[1];
    rest = schemeMatch[2];
  }
  // Authority (host) = chars after `scheme://` up to the first '/' or '?'.
  // Real expo-linking sits on NSURLComponents, which exposes `.host`; the
  // Direction-B return_url allowlist check (parseBuyVpnRequest, #629) relies
  // on it, so the mock must extract it too (previously hardcoded null → the
  // valid-request test failed).
  let hostname: string | null = null;
  let afterHost = rest;
  const hostMatch = rest.match(/^([^/?]*)(.*)$/);
  if (hostMatch) {
    hostname = hostMatch[1] || null;
    afterHost = hostMatch[2];
  }
  let path: string | null = null;
  let query = '';
  const qIdx = afterHost.indexOf('?');
  if (qIdx >= 0) {
    path = afterHost.slice(0, qIdx) || null;
    query = afterHost.slice(qIdx + 1);
  } else {
    path = afterHost || null;
  }
  const queryParams: Record<string, string> = {};
  if (query) {
    // Delegate to URLSearchParams so `+`→space and percent-encoded
    // characters match the real expo-linking parser behaviour (which
    // sits on top of NSURLComponents on iOS — same encoding rules).
    const usp = new URLSearchParams(query);
    for (const [k, v] of usp) {
      queryParams[k] = v;
    }
  }
  return { scheme, hostname, path, queryParams };
}

// -----------------------------------------------------------------------
// Test helpers (not part of the production API surface)
// -----------------------------------------------------------------------

/** Force canOpenURL to return `value` for a specific URL/scheme. */
export function __setCanOpen(url: string, value: boolean): void {
  canOpenMap.set(url, value);
}

/** Set the default canOpenURL response when no per-URL override exists. */
export function __setCanOpenDefault(value: boolean): void {
  canOpenDefault = value;
}

/** Get the list of URLs that were openURL()'d since the last reset. */
export function __getOpenHistory(): readonly string[] {
  return openHistory;
}

/** Synthesise a deeplink callback into all registered listeners. */
export function __fireUrl(url: string): void {
  for (const l of [...urlListeners]) {
    l({ url });
  }
}

/** Reset all mock state between tests. */
export function __reset(): void {
  canOpenMap = new Map();
  canOpenDefault = true;
  openHistory.length = 0;
  urlListeners.length = 0;
}
