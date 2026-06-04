// Tests for the region-picker shaping logic (Refs #592).
//
// vpn-svc hands the app a flat slug list; the picker's whole information
// architecture (continent → country → city, searchable) is built by the
// three pure functions under test. The edge cases here are the ones a
// real region list WILL hit: an unrecognized prefix (must still render,
// not vanish), two prefixes that map to one country (uk + gb → United
// Kingdom — must merge, not double), city sort + continent order
// (determines what the user scans), and the search filter's "country hit
// keeps every city, city hit keeps only matches" rule. All pure — driven
// directly, no render. The RegionRow import is type-only (no native code).

import type { RegionRow } from '../coordinator';
import {
  CONTINENT_ORDER,
  filterGroups,
  groupRegions,
  regionToCity,
  type CountryGroup,
} from '../region-grouping';

const row = (region: string, healthy = 1, total = 1): RegionRow => ({
  region,
  healthyProviders: healthy,
  totalProviders: total,
});

describe('regionToCity', () => {
  it('maps a known prefix to its country + title-cased city label', () => {
    const { country, city } = regionToCity(row('us-east-1', 3, 5));
    expect(country.code).toBe('US');
    expect(country.continent).toBe('AMERICAS');
    expect(city.city).toBe('East 1');
    expect(city.slug).toBe('us-east-1');
    expect(city.healthy).toBe(3);
    expect(city.total).toBe(5);
    expect(city.pingMs).toBeNull();
  });

  it('falls back to an OTHER-continent group for an unknown prefix (never vanishes)', () => {
    const { country, city } = regionToCity(row('xx-foo-1'));
    expect(country.code).toBe('__');
    expect(country.name).toBe('XX'); // upper-cased prefix
    expect(country.flag).toBe('🌐');
    expect(country.continent).toBe('OTHER');
    expect(city.city).toBe('Foo 1');
  });

  it('uses the country name as the city label when the slug has no city part', () => {
    const { city } = regionToCity(row('de'));
    expect(city.city).toBe('Germany');
  });

  it('honours the "ap" regional special-case', () => {
    const { country } = regionToCity(row('ap-southeast-1'));
    expect(country.name).toBe('Asia Pacific');
    expect(country.continent).toBe('ASIA-PACIFIC');
  });
});

describe('groupRegions', () => {
  it('merges cities under one country and sorts them alphabetically', () => {
    const groups = groupRegions([row('us-west-1'), row('us-east-1')]);
    expect(groups).toHaveLength(1);
    expect(groups[0].code).toBe('US');
    expect(groups[0].cities.map((c) => c.city)).toEqual(['East 1', 'West 1']);
  });

  it('merges two prefixes that resolve to the same country (uk + gb → United Kingdom)', () => {
    const groups = groupRegions([row('uk-lon-1'), row('gb-man-1')]);
    expect(groups).toHaveLength(1);
    expect(groups[0].name).toBe('United Kingdom');
    expect(groups[0].cities.map((c) => c.city)).toEqual(['Lon 1', 'Man 1']);
  });

  it('orders countries by the canonical continent order, then by name', () => {
    const groups = groupRegions([
      row('au-syd-1'), // OCEANIA
      row('jp-tok-1'), // ASIA-PACIFIC
      row('us-east-1'), // AMERICAS
      row('de-fra-1'), // EUROPE
      row('za-jnb-1'), // AFRICA
      row('xx-foo-1'), // OTHER
    ]);
    const continents = groups.map((g) => g.continent);
    expect(continents).toEqual(['EUROPE', 'AMERICAS', 'ASIA-PACIFIC', 'AFRICA', 'OCEANIA', 'OTHER']);
    // and the order matches the exported canonical list (minus any absent)
    expect(continents).toEqual(CONTINENT_ORDER.filter((c) => continents.includes(c)));
  });

  it('sorts same-continent countries by name (France before Germany)', () => {
    const groups = groupRegions([row('de-fra-1'), row('fr-par-1')]);
    expect(groups.map((g) => g.name)).toEqual(['France', 'Germany']);
  });

  it('returns [] for an empty region list', () => {
    expect(groupRegions([])).toEqual([]);
  });
});

describe('filterGroups', () => {
  const groups: CountryGroup[] = groupRegions([
    row('us-east-1'),
    row('us-west-1'),
    row('de-fra-1'),
  ]);

  it('returns the groups unchanged for an empty search', () => {
    expect(filterGroups(groups, '')).toBe(groups);
  });

  it('keeps every city when the COUNTRY name matches', () => {
    const out = filterGroups(groups, 'united states');
    expect(out).toHaveLength(1);
    expect(out[0].cities).toHaveLength(2); // both us cities retained
  });

  it('keeps only the matching cities when only a CITY matches', () => {
    const out = filterGroups(groups, 'east');
    expect(out).toHaveLength(1);
    expect(out[0].code).toBe('US');
    expect(out[0].cities.map((c) => c.city)).toEqual(['East 1']);
  });

  it('drops countries with no country- or city-level match', () => {
    const out = filterGroups(groups, 'tokyo');
    expect(out).toEqual([]);
  });

  it('matches on the country code and is case-insensitive', () => {
    const out = filterGroups(groups, 'DE');
    // 'de' hits Germany's code AND 'us-east' slugs contain no 'de' — but
    // Germany's code is 'DE', so Germany is kept whole.
    expect(out.some((g) => g.name === 'Germany')).toBe(true);
  });
});
