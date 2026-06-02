// Region picker — SectionList grouped by continent (#592).
//
// Wireframe ref: mobile/ios/docs/ux-wireframes-v2.md Screen 8.
//
// "Best (auto)" pinned at top in its own slot, outside the SectionList.
// Countries are grouped under continent labels (EUROPE / AMERICAS /
// ASIA-PACIFIC). Tapping a country expands inline city rows with
// individual ping numbers.
//
// Search filters country/city/slug across all sections; "Best (auto)"
// remains visible regardless of the filter.

import { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  SectionList,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { RegionRow } from '@/components/region-row';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Radii, Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { listRegions, type RegionRow as Region } from '@/lib/coordinator';

const SELECTED_REGION_KEY = 'iogrid.region.selected';
/** Sentinel persisted to AsyncStorage when "Best (auto)" is selected. */
export const AUTO_REGION_SENTINEL = 'auto';

interface CityRow {
  slug: string;
  label: string;
  pingMs: number;
}

interface CountryRow {
  code: string;
  flag?: string;
  name: string;
  cities: CityRow[];
  pingMs: number;
}

interface Section {
  title: string;
  data: CountryRow[];
}

const CONTINENT_ORDER = ['EUROPE', 'AMERICAS', 'ASIA-PACIFIC', 'OTHER'] as const;
type Continent = (typeof CONTINENT_ORDER)[number];

export default function RegionsScreen() {
  const theme = useTheme();
  const [rows, setRows] = useState<Region[] | null>(null);
  const [selected, setSelected] = useState<string>(AUTO_REGION_SENTINEL);
  const [search, setSearch] = useState<string>('');
  const [refreshing, setRefreshing] = useState<boolean>(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const refresh = async () => {
    setRefreshing(true);
    setLoadError(null);
    try {
      const data = await listRegions();
      setRows(data);
    } catch (e: unknown) {
      setLoadError(e instanceof Error ? e.message : String(e));
      setRows([]);
    } finally {
      setRefreshing(false);
    }
  };

  useEffect(() => {
    refresh();
    AsyncStorage.getItem(SELECTED_REGION_KEY)
      .then((v) => v && setSelected(v))
      .catch(() => undefined);
  }, []);

  const onPick = async (value: string) => {
    setSelected(value);
    try {
      await AsyncStorage.setItem(SELECTED_REGION_KEY, value);
    } catch {
      // Persistence failure is non-fatal — in-memory selection is used
      // for the current app session.
    }
  };

  const toggleExpand = (code: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(code)) next.delete(code);
      else next.add(code);
      return next;
    });
  };

  const sections: Section[] = useMemo(() => {
    const byContinent: Record<Continent, CountryRow[]> = {
      EUROPE: [],
      AMERICAS: [],
      'ASIA-PACIFIC': [],
      OTHER: [],
    };
    const needle = search.trim().toLowerCase();

    for (const r of rows ?? []) {
      const country = countryForRegion(r.region);
      const cityLabel = cityForRegion(r.region);
      const cityRow: CityRow = {
        slug: r.region,
        label: cityLabel,
        pingMs: pingForRegion(r.region),
      };
      if (
        needle &&
        ![country.name, cityLabel, r.region].some((s) =>
          s.toLowerCase().includes(needle),
        )
      ) {
        continue;
      }
      const list = byContinent[country.continent];
      let existing = list.find((c) => c.code === country.code);
      if (!existing) {
        existing = {
          code: country.code,
          flag: country.flag,
          name: country.name,
          cities: [],
          pingMs: 0,
        };
        list.push(existing);
      }
      existing.cities.push(cityRow);
      existing.pingMs = Math.round(
        existing.cities.reduce((acc, c) => acc + c.pingMs, 0) /
          existing.cities.length,
      );
    }

    return CONTINENT_ORDER.flatMap((cont) =>
      byContinent[cont].length > 0
        ? [{ title: cont, data: byContinent[cont] }]
        : [],
    );
  }, [rows, search]);

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe} edges={['bottom']}>
        <View style={styles.searchWrap}>
          <TextInput
            style={[
              styles.search,
              {
                backgroundColor: theme.backgroundElement,
                color: theme.text,
              },
            ]}
            placeholder="Search country, city, or slug"
            placeholderTextColor={theme.textTertiary}
            value={search}
            onChangeText={setSearch}
            autoCapitalize="none"
            autoCorrect={false}
            testID="region-search"
          />
        </View>

        {/* Best (auto) pinned at top, outside SectionList */}
        <View style={styles.pinnedAuto}>
          <RegionRow
            testID="region-best-auto"
            flag="🌐"
            label="Best (auto)"
            subtitle="Coordinator picks the closest fastest provider"
            selected={selected === AUTO_REGION_SENTINEL}
            onPress={() => onPick(AUTO_REGION_SENTINEL)}
          />
        </View>

        {loadError && rows?.length === 0 && (
          <ThemedText type="body-s" color={theme.error} style={styles.error}>
            Couldn&apos;t fetch the full list — only the default option is
            available. Pull to retry.
          </ThemedText>
        )}

        <SectionList
          sections={sections}
          keyExtractor={(item) => item.code}
          stickySectionHeadersEnabled={false}
          contentContainerStyle={styles.listContent}
          refreshControl={
            <RefreshControl refreshing={refreshing} onRefresh={refresh} />
          }
          ListEmptyComponent={
            refreshing ? (
              <ActivityIndicator style={{ paddingVertical: 24 }} />
            ) : null
          }
          renderSectionHeader={({ section }) => (
            <ThemedText
              type="caption"
              color={theme.textSecondary}
              style={styles.sectionHeader}
            >
              {section.title}
            </ThemedText>
          )}
          renderItem={({ item }) => {
            const isExpanded = expanded.has(item.code);
            const isSelected = item.cities.some((c) => c.slug === selected);
            return (
              <View style={styles.countryGroup}>
                <RegionRow
                  testID={`region-country-${item.code}`}
                  flag={item.flag}
                  label={item.name}
                  subtitle={`${item.cities.length} ${
                    item.cities.length === 1 ? 'city' : 'cities'
                  } • ${item.pingMs}ms`}
                  selected={isSelected}
                  expandable
                  expanded={isExpanded}
                  onPress={() => toggleExpand(item.code)}
                />
                {isExpanded
                  ? item.cities.map((city) => (
                      <View key={city.slug} style={styles.cityRow}>
                        <Pressable
                          testID={`region-city-${city.slug}`}
                          accessibilityRole="button"
                          onPress={() => onPick(city.slug)}
                          style={({ pressed }) => [
                            styles.cityInner,
                            { backgroundColor: theme.backgroundElement },
                            pressed && { backgroundColor: theme.backgroundSelected },
                          ]}
                        >
                          <ThemedText type="body-m">{city.label}</ThemedText>
                          <View style={styles.cityRight}>
                            <ThemedText
                              type="body-s"
                              color={theme.textSecondary}
                              style={styles.pingText}
                            >
                              {city.pingMs}ms
                            </ThemedText>
                            {selected === city.slug ? (
                              <ThemedText type="body-m" color={theme.accent}>
                                ✓
                              </ThemedText>
                            ) : null}
                          </View>
                        </Pressable>
                      </View>
                    ))
                  : null}
              </View>
            );
          }}
        />
      </SafeAreaView>
    </ThemedView>
  );
}

// ── Region metadata helpers ──────────────────────────────────────────

interface CountryMeta {
  code: string;
  name: string;
  flag?: string;
  continent: Continent;
}

const COUNTRIES: Record<string, CountryMeta> = {
  us: { code: 'us', name: 'United States', flag: '🇺🇸', continent: 'AMERICAS' },
  ca: { code: 'ca', name: 'Canada', flag: '🇨🇦', continent: 'AMERICAS' },
  br: { code: 'br', name: 'Brazil', flag: '🇧🇷', continent: 'AMERICAS' },
  de: { code: 'de', name: 'Germany', flag: '🇩🇪', continent: 'EUROPE' },
  fr: { code: 'fr', name: 'France', flag: '🇫🇷', continent: 'EUROPE' },
  nl: { code: 'nl', name: 'Netherlands', flag: '🇳🇱', continent: 'EUROPE' },
  uk: { code: 'uk', name: 'United Kingdom', flag: '🇬🇧', continent: 'EUROPE' },
  eu: { code: 'eu', name: 'Europe', flag: '🇪🇺', continent: 'EUROPE' },
  ie: { code: 'ie', name: 'Ireland', flag: '🇮🇪', continent: 'EUROPE' },
  jp: { code: 'jp', name: 'Japan', flag: '🇯🇵', continent: 'ASIA-PACIFIC' },
  sg: { code: 'sg', name: 'Singapore', flag: '🇸🇬', continent: 'ASIA-PACIFIC' },
  au: { code: 'au', name: 'Australia', flag: '🇦🇺', continent: 'ASIA-PACIFIC' },
  ap: { code: 'ap', name: 'Asia Pacific', flag: '🌏', continent: 'ASIA-PACIFIC' },
};

function countryForRegion(slug: string): CountryMeta {
  const prefix = slug.split('-')[0];
  if (prefix === 'eu') {
    if (slug.includes('west')) return COUNTRIES.ie;
    if (slug.includes('central')) return COUNTRIES.de;
    return COUNTRIES.eu;
  }
  return (
    COUNTRIES[prefix] ?? {
      code: prefix || 'unknown',
      name: prefix.toUpperCase() || 'Other',
      continent: 'OTHER',
    }
  );
}

function cityForRegion(slug: string): string {
  const map: Record<string, string> = {
    'us-east-1': 'Virginia',
    'us-east-2': 'Ohio',
    'us-west-1': 'California',
    'us-west-2': 'Oregon',
    'eu-west-1': 'Ireland',
    'eu-central-1': 'Frankfurt',
    'ap-northeast-1': 'Tokyo',
    'ap-southeast-1': 'Singapore',
  };
  if (map[slug]) return map[slug];
  return slug
    .split('-')
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join(' ');
}

// Stable ping estimates per region for the v1 wireframe. Replaced by
// real per-provider RTT once Track 3 wires the probe response.
function pingForRegion(slug: string): number {
  const hash = Array.from(slug).reduce((acc, c) => acc + c.charCodeAt(0), 0);
  return 20 + (hash % 80);
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1 },
  searchWrap: { paddingHorizontal: Spacing.lg, paddingVertical: Spacing.md },
  search: {
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.md,
    borderRadius: Radii.md,
    fontSize: 16,
  },
  pinnedAuto: { paddingHorizontal: Spacing.lg, paddingBottom: Spacing.md },
  error: { paddingHorizontal: Spacing.lg, paddingVertical: Spacing.sm },
  listContent: {
    paddingHorizontal: Spacing.lg,
    paddingBottom: Spacing.xl,
    gap: Spacing.sm,
  },
  sectionHeader: {
    paddingTop: Spacing.lg,
    paddingBottom: Spacing.sm,
    textTransform: 'uppercase',
    letterSpacing: 1.5,
  },
  countryGroup: { gap: Spacing.sm },
  cityRow: { paddingLeft: Spacing.lg },
  cityInner: {
    paddingVertical: Spacing.md,
    paddingHorizontal: Spacing.lg,
    borderRadius: Radii.md,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  cityRight: { flexDirection: 'row', alignItems: 'center', gap: Spacing.md },
  pingText: { fontVariant: ['tabular-nums'] },
});
