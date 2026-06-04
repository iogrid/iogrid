/**
 * Pure region-list shaping for the region picker (Refs #592).
 *
 * vpn-svc returns a flat list of region slugs (`us-east-1`, `de-fra-1`,
 * …); the picker shows them grouped continent → country → city, sorted,
 * and filtered by a search box. That shaping is non-trivial — prefix→
 * country inference with a fallback, an `'ap'` regional special-case,
 * city-label title-casing, a stable continent order, and a search filter
 * whose "country hit keeps all cities, city hit keeps only matches"
 * semantics are easy to regress. None of it was tested while it lived
 * inside the component's useMemos.
 *
 * Extracted here (pure, no React / no native imports) so it's covered by
 * jest directly — same approach as connection-steps + coordinator. The
 * `RegionRow` import is type-only, so this module pulls in no native code.
 *
 * Refs #580, #592.
 */

import type { RegionRow } from '@/lib/coordinator';

export type Continent =
  | 'EUROPE'
  | 'AMERICAS'
  | 'ASIA-PACIFIC'
  | 'AFRICA'
  | 'OCEANIA'
  | 'OTHER';

export interface CityRow {
  /** Region slug we send to vpn-svc (e.g. 'us-east-1'). */
  slug: string;
  /** Human-readable city label. */
  city: string;
  /** Round-trip ping ms, or null if unknown. */
  pingMs: number | null;
  /** Number of healthy providers in this region. */
  healthy: number;
  /** Total providers (healthy + offline). */
  total: number;
}

export interface CountryGroup {
  /** ISO 3166-1 alpha-2 country code. */
  code: string;
  /** Country name. */
  name: string;
  /** Emoji flag. */
  flag: string;
  /** Continent for section grouping. */
  continent: Continent;
  /** Cities under this country. */
  cities: CityRow[];
  /** Median ping across cities, for the country row's subtitle. */
  medianPingMs: number | null;
}

type CountryMeta = Pick<CountryGroup, 'code' | 'name' | 'flag' | 'continent'>;

// vpn-svc returns region slugs like 'us-east-1'; we infer the country
// code + continent from the prefix. This stays a heuristic until the
// backend ships proper geo metadata in the regions response.
export const PREFIX_TO_COUNTRY: Record<string, CountryMeta> = {
  us: { code: 'US', name: 'United States', flag: '🇺🇸', continent: 'AMERICAS' },
  ca: { code: 'CA', name: 'Canada', flag: '🇨🇦', continent: 'AMERICAS' },
  br: { code: 'BR', name: 'Brazil', flag: '🇧🇷', continent: 'AMERICAS' },
  eu: { code: 'EU', name: 'Europe', flag: '🇪🇺', continent: 'EUROPE' },
  de: { code: 'DE', name: 'Germany', flag: '🇩🇪', continent: 'EUROPE' },
  fr: { code: 'FR', name: 'France', flag: '🇫🇷', continent: 'EUROPE' },
  uk: { code: 'GB', name: 'United Kingdom', flag: '🇬🇧', continent: 'EUROPE' },
  gb: { code: 'GB', name: 'United Kingdom', flag: '🇬🇧', continent: 'EUROPE' },
  nl: { code: 'NL', name: 'Netherlands', flag: '🇳🇱', continent: 'EUROPE' },
  es: { code: 'ES', name: 'Spain', flag: '🇪🇸', continent: 'EUROPE' },
  it: { code: 'IT', name: 'Italy', flag: '🇮🇹', continent: 'EUROPE' },
  jp: { code: 'JP', name: 'Japan', flag: '🇯🇵', continent: 'ASIA-PACIFIC' },
  sg: { code: 'SG', name: 'Singapore', flag: '🇸🇬', continent: 'ASIA-PACIFIC' },
  kr: { code: 'KR', name: 'South Korea', flag: '🇰🇷', continent: 'ASIA-PACIFIC' },
  hk: { code: 'HK', name: 'Hong Kong', flag: '🇭🇰', continent: 'ASIA-PACIFIC' },
  au: { code: 'AU', name: 'Australia', flag: '🇦🇺', continent: 'OCEANIA' },
  ap: { code: '__', name: 'Asia Pacific', flag: '🌏', continent: 'ASIA-PACIFIC' },
  za: { code: 'ZA', name: 'South Africa', flag: '🇿🇦', continent: 'AFRICA' },
};

export const CONTINENT_ORDER: Continent[] = [
  'EUROPE',
  'AMERICAS',
  'ASIA-PACIFIC',
  'AFRICA',
  'OCEANIA',
  'OTHER',
];

/**
 * Map a single region row to its country metadata + a city row. Unknown
 * prefixes fall back to an OTHER-continent group keyed on the upper-cased
 * prefix (or the full slug if the prefix is empty), so a region the
 * heuristic doesn't recognize still renders rather than vanishing.
 */
export function regionToCity(row: RegionRow): { country: CountryMeta; city: CityRow } {
  const parts = row.region.split('-');
  const prefix = parts[0]?.toLowerCase() ?? '';
  const country: CountryMeta =
    PREFIX_TO_COUNTRY[prefix] ?? {
      code: '__',
      name: prefix.toUpperCase() || row.region,
      flag: '🌐',
      continent: 'OTHER',
    };
  // Title-case the remainder as the city label (e.g. 'us-east-1' →
  // 'East 1'). When the backend ships city metadata, swap this for
  // row.city || derived.
  const cityLabel = parts
    .slice(1)
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join(' ');
  return {
    country,
    city: {
      slug: row.region,
      city: cityLabel || country.name,
      pingMs: null,
      healthy: row.healthyProviders,
      total: row.totalProviders,
    },
  };
}

/**
 * Group a flat region list into countries (cities sorted alphabetically),
 * ordered by continent then country name. Countries are keyed on
 * `code:name` so distinct fallback groups don't collide.
 */
export function groupRegions(rows: RegionRow[]): CountryGroup[] {
  const byCountry = new Map<string, CountryGroup>();
  for (const row of rows) {
    const { country, city } = regionToCity(row);
    const key = country.code + ':' + country.name;
    const existing = byCountry.get(key);
    if (existing) {
      existing.cities.push(city);
    } else {
      byCountry.set(key, { ...country, cities: [city], medianPingMs: null });
    }
  }
  for (const g of byCountry.values()) {
    g.cities.sort((a, b) => a.city.localeCompare(b.city));
  }
  const arr = Array.from(byCountry.values());
  arr.sort((a, b) => {
    const ca = CONTINENT_ORDER.indexOf(a.continent);
    const cb = CONTINENT_ORDER.indexOf(b.continent);
    if (ca !== cb) return ca - cb;
    return a.name.localeCompare(b.name);
  });
  return arr;
}

/**
 * Filter grouped countries by a search needle. A country-name/code hit
 * keeps the whole country (all its cities); otherwise a country is kept
 * only with the cities whose label or slug match. Empty search returns
 * the groups unchanged.
 */
export function filterGroups(groups: CountryGroup[], search: string): CountryGroup[] {
  if (!search) return groups;
  const needle = search.toLowerCase();
  return groups
    .map((g) => {
      const countryHit =
        g.name.toLowerCase().includes(needle) || g.code.toLowerCase().includes(needle);
      const matchingCities = g.cities.filter(
        (c) => c.city.toLowerCase().includes(needle) || c.slug.toLowerCase().includes(needle),
      );
      if (countryHit) return g;
      if (matchingCities.length > 0) return { ...g, cities: matchingCities };
      return null;
    })
    .filter((g): g is CountryGroup => g !== null);
}
