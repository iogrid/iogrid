/**
 * Region picker — v2 rewrite per mobile/ios/docs/ux-wireframes-v2.md
 * Screen 8. Continent-grouped country list, expandable to city rows
 * with per-row ping latency, search bar at top, 'Best (auto)' pinned.
 *
 * Selection persists to AsyncStorage so the main screen reads the
 * same key. Tapping a country expands inline; tapping a city selects
 * + pops back to /index for the next connect attempt.
 *
 * Refs #580, #592.
 */

import { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { GlobeIcon } from '@/components/icons';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Card, Radii, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { listRegions, type RegionRow } from '@/lib/coordinator';
import {
  CONTINENT_ORDER,
  filterGroups,
  groupRegions,
  type CountryGroup,
} from '@/lib/region-grouping';

const SELECTED_REGION_KEY = 'iogrid.region.selected';
export const AUTO_REGION_SENTINEL = 'auto';

export default function RegionsScreen() {
  const theme = useTheme();
  const [rows, setRows] = useState<RegionRow[] | null>(null);
  const [selected, setSelected] = useState<string>(AUTO_REGION_SENTINEL);
  const [search, setSearch] = useState<string>('');
  const [refreshing, setRefreshing] = useState<boolean>(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [expandedCountry, setExpandedCountry] = useState<string | null>(null);

  // ── Initial load + selected restore ─────────────────────────────
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
    void refresh();
    AsyncStorage.getItem(SELECTED_REGION_KEY)
      .then((v) => v && setSelected(v))
      .catch(() => undefined);
  }, []);

  // ── Group rows into continents → countries → cities ─────────────
  // (pure shaping lives in @/lib/region-grouping — unit-tested there)
  const groups: CountryGroup[] = useMemo(() => (rows ? groupRegions(rows) : []), [rows]);

  // ── Search filter ───────────────────────────────────────────────
  const filteredGroups: CountryGroup[] = useMemo(
    () => filterGroups(groups, search),
    [groups, search],
  );

  // ── Group filtered by continent for rendering ───────────────────
  const byContinent = useMemo(() => {
    const map = new Map<CountryGroup['continent'], CountryGroup[]>();
    for (const c of CONTINENT_ORDER) map.set(c, []);
    for (const g of filteredGroups) map.get(g.continent)?.push(g);
    return map;
  }, [filteredGroups]);

  // ── Selection ───────────────────────────────────────────────────
  const onPick = async (slug: string, displayLabel?: string) => {
    setSelected(slug);
    try {
      await AsyncStorage.setItem(SELECTED_REGION_KEY, slug);
      if (displayLabel) {
        await AsyncStorage.setItem(SELECTED_REGION_KEY + '.label', displayLabel);
      }
    } catch {
      // ignore
    }
    router.back();
  };

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'left', 'right']}>
        {/* ── Nav header + search ──────────────────────────────── */}
        <View style={styles.navHeader}>
          <Pressable
            testID="regions-back"
            onPress={() => router.back()}
            hitSlop={12}
            accessibilityLabel="Close region picker"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.navLeading, { color: theme.textSecondary }]}>
              ‹ Back
            </ThemedText>
          </Pressable>
          <ThemedText style={[styles.navTitle, { color: theme.text }]}>
            Choose region
          </ThemedText>
          <View style={styles.navTrailing} />
        </View>

        <View
          style={[
            styles.searchBox,
            { backgroundColor: theme.backgroundElement, borderColor: theme.border },
          ]}
        >
          <ThemedText style={[styles.searchIcon, { color: theme.textTertiary }]}>
            ⌕
          </ThemedText>
          <TextInput
            testID="regions-search"
            style={[styles.searchInput, { color: theme.text }]}
            placeholder="Search countries or cities"
            placeholderTextColor={theme.textTertiary}
            value={search}
            onChangeText={setSearch}
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="search"
            clearButtonMode="while-editing"
          />
        </View>

        <ScrollView
          contentContainerStyle={styles.scroll}
          refreshControl={
            <RefreshControl refreshing={refreshing} onRefresh={refresh} />
          }
          showsVerticalScrollIndicator={false}
        >
          {/* ── Best (auto) — always pinned at top ───────────────── */}
          <View
            style={[
              styles.sectionGroup,
              { backgroundColor: theme.backgroundCard, borderColor: theme.border },
            ]}
          >
            <Pressable
              testID="region-best-auto"
              onPress={() => onPick(AUTO_REGION_SENTINEL, 'Best (auto)')}
              accessibilityLabel="Best auto-pick"
              accessibilityRole="button"
              style={({ pressed }) => [
                styles.bestAutoRow,
                pressed ? { opacity: 0.7 } : null,
              ]}
            >
              <View style={styles.bestAutoText}>
                <View style={styles.bestAutoTitleRow}>
                  {/* drawn globe, not the 🌐 emoji — same monochrome rule
                      that replaced ⚙/🏠 (pass 4, #684) */}
                  <GlobeIcon size={20} color={theme.text} />
                  <ThemedText style={[styles.bestAutoTitle, { color: theme.text }]}>
                    Best (auto)
                  </ThemedText>
                </View>
                <ThemedText style={[styles.bestAutoSub, { color: theme.textSecondary }]}>
                  Coordinator picks the closest, fastest peer
                </ThemedText>
              </View>
              {selected === AUTO_REGION_SENTINEL ? (
                <ThemedText style={[styles.checkmark, { color: theme.accent }]}>✓</ThemedText>
              ) : null}
            </Pressable>
          </View>

          {/* ── Load error banner (non-blocking; auto still works) ── */}
          {loadError && (rows?.length ?? 0) === 0 ? (
            <ThemedText
              style={[styles.errorBanner, { color: theme.warning, borderColor: theme.warning }]}
            >
              Couldn't fetch the full region list. Only the default option is available.
              Pull down to retry.
            </ThemedText>
          ) : null}

          {refreshing && (rows?.length ?? 0) === 0 ? (
            <ActivityIndicator style={styles.spinner} color={theme.textSecondary} />
          ) : null}

          {/* ── Continent sections ───────────────────────────────── */}
          {CONTINENT_ORDER.map((continent) => {
            const list = byContinent.get(continent) ?? [];
            if (list.length === 0) return null;
            return (
              <View key={continent}>
                <ThemedText
                  style={[styles.sectionHeader, { color: theme.textTertiary }]}
                >
                  {continent}
                </ThemedText>
                <View
                  style={[
                    styles.sectionGroup,
                    { backgroundColor: theme.backgroundCard, borderColor: theme.border },
                  ]}
                >
                  {list.map((g, i) => {
                    const isExpanded = expandedCountry === g.code + ':' + g.name;
                    const last = i === list.length - 1;
                    return (
                      <View key={g.code + ':' + g.name}>
                        <Pressable
                          testID={`region-country-${g.code.toLowerCase()}`}
                          onPress={() =>
                            setExpandedCountry(isExpanded ? null : g.code + ':' + g.name)
                          }
                          accessibilityLabel={`${g.name} — ${g.cities.length} cities`}
                          accessibilityRole="button"
                          style={({ pressed }) => [
                            styles.countryRow,
                            {
                              borderBottomColor: theme.border,
                              borderBottomWidth:
                                isExpanded || last ? 0 : StyleSheet.hairlineWidth,
                            },
                            pressed ? { opacity: 0.7 } : null,
                          ]}
                        >
                          <ThemedText style={styles.flag}>{g.flag}</ThemedText>
                          <View style={styles.countryText}>
                            <ThemedText
                              style={[styles.countryName, { color: theme.text }]}
                            >
                              {g.name}
                            </ThemedText>
                            <ThemedText
                              style={[styles.countrySub, { color: theme.textSecondary }]}
                            >
                              {g.cities.length}
                              {g.cities.length === 1 ? ' city' : ' cities'}
                              {g.medianPingMs != null ? ` · ${g.medianPingMs} ms` : ''}
                            </ThemedText>
                          </View>
                          <ThemedText
                            style={[
                              styles.expandChevron,
                              { color: theme.textTertiary },
                              isExpanded ? { transform: [{ rotate: '90deg' }] } : null,
                            ]}
                          >
                            ›
                          </ThemedText>
                        </Pressable>

                        {/* Expanded city rows */}
                        {isExpanded
                          ? g.cities.map((c, j) => {
                              const cityLast = j === g.cities.length - 1;
                              return (
                                <Pressable
                                  key={c.slug}
                                  testID={`region-city-${c.slug}`}
                                  onPress={() =>
                                    onPick(c.slug, `${g.name} — ${c.city}`)
                                  }
                                  accessibilityLabel={`${g.name}, ${c.city}`}
                                  accessibilityRole="button"
                                  style={({ pressed }) => [
                                    styles.cityRow,
                                    {
                                      borderBottomColor: theme.border,
                                      borderBottomWidth:
                                        cityLast && last ? 0 : StyleSheet.hairlineWidth,
                                      backgroundColor:
                                        selected === c.slug
                                          ? theme.backgroundSelected
                                          : 'transparent',
                                    },
                                    pressed ? { opacity: 0.7 } : null,
                                  ]}
                                >
                                  <View style={styles.cityText}>
                                    <ThemedText
                                      style={[styles.cityName, { color: theme.text }]}
                                    >
                                      {c.city}
                                    </ThemedText>
                                    <ThemedText
                                      style={[
                                        styles.citySub,
                                        { color: theme.textSecondary },
                                      ]}
                                    >
                                      {c.healthy} of {c.total} online
                                      {c.pingMs != null ? ` · ${c.pingMs} ms` : ''}
                                    </ThemedText>
                                  </View>
                                  {selected === c.slug ? (
                                    <ThemedText
                                      style={[styles.checkmark, { color: theme.accent }]}
                                    >
                                      ✓
                                    </ThemedText>
                                  ) : null}
                                </Pressable>
                              );
                            })
                          : null}
                      </View>
                    );
                  })}
                </View>
              </View>
            );
          })}
        </ScrollView>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  safe: { flex: 1 },
  navHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.md,
  },
  navLeading: {
    ...TypeScale.bodyM,
    fontWeight: '500',
    minWidth: 80,
  },
  navTitle: {
    ...TypeScale.bodyL,
    fontWeight: '600',
  },
  navTrailing: {
    minWidth: 80,
  },
  searchBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.sm,
    marginHorizontal: Spacing.lg,
    marginBottom: Spacing.md,
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.sm,
    borderRadius: Radii.md,
    borderWidth: StyleSheet.hairlineWidth,
  },
  searchIcon: {
    fontSize: 18,
  },
  searchInput: {
    flex: 1,
    ...TypeScale.bodyM,
  },
  scroll: {
    paddingBottom: Spacing.xxxl,
  },
  sectionHeader: {
    ...TypeScale.captionStrong,
    letterSpacing: 1.5,
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.sm,
  },
  sectionGroup: {
    marginHorizontal: Spacing.lg,
    borderRadius: Radii.lg,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: 'hidden',
  },
  bestAutoRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Card.padding,
    paddingVertical: Spacing.lg,
    minHeight: 64,
  },
  bestAutoText: {
    flex: 1,
    paddingRight: Spacing.md,
  },
  bestAutoTitleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  bestAutoTitle: {
    ...TypeScale.bodyL,
    fontWeight: '600',
  },
  bestAutoSub: {
    ...TypeScale.bodyS,
    marginTop: 2,
  },
  countryRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Card.padding,
    paddingVertical: Spacing.md,
    gap: Spacing.md,
    minHeight: 56,
  },
  flag: {
    fontSize: 24,
  },
  countryText: {
    flex: 1,
  },
  countryName: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  countrySub: {
    ...TypeScale.bodyS,
    marginTop: 2,
  },
  expandChevron: {
    fontSize: 22,
    fontWeight: '300',
  },
  cityRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Card.padding,
    paddingLeft: Card.padding + 36 + Spacing.md, // align under flag column
    paddingVertical: Spacing.md,
    minHeight: 52,
  },
  cityText: {
    flex: 1,
  },
  cityName: {
    ...TypeScale.bodyL,
  },
  citySub: {
    ...TypeScale.bodyS,
    marginTop: 2,
  },
  checkmark: {
    fontSize: 20,
    fontWeight: '700',
  },
  errorBanner: {
    ...TypeScale.bodyS,
    marginHorizontal: Spacing.lg,
    marginTop: Spacing.md,
    padding: Spacing.md,
    borderRadius: Radii.md,
    borderWidth: StyleSheet.hairlineWidth,
  },
  spinner: {
    marginVertical: Spacing.xl,
  },
});
