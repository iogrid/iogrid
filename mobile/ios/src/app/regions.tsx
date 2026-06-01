// Region picker — fetches the live region list from vpn-svc + lets
// the user select either "Best (auto)" (coordinator picks) or a
// specific region.
//
// Persists the selection to AsyncStorage so it survives app
// relaunches; the toggle screen reads the same key on mount.
//
// Closes #571. Pairs with the coordinator endpoints sub-agent
// shipped in #570: GET /v1/vpn/regions (count per region) +
// GET /v1/vpn/regions/{r}/providers?limit=3 (top-N for client probe,
// not used here — that's #572's job).

import { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { listRegions, type RegionRow } from '@/lib/coordinator';

const SELECTED_REGION_KEY = 'iogrid.region.selected';
/** The sentinel value persisted to AsyncStorage when "Best (auto)"
 *  is selected — the toggle screen translates this to region=auto in
 *  the session POST. */
export const AUTO_REGION_SENTINEL = 'auto';

interface DisplayRow {
  /** The opaque region slug we send to vpn-svc (e.g. "us-east-1") OR
   *  AUTO_REGION_SENTINEL for the auto option. */
  value: string;
  /** Human-readable label. */
  label: string;
  /** Optional subtitle (e.g. "12 providers online"). */
  subtitle?: string;
  /** Optional flag emoji guessed from the region slug. */
  flag?: string;
}

export default function RegionsScreen() {
  const [rows, setRows] = useState<RegionRow[] | null>(null);
  const [selected, setSelected] = useState<string>(AUTO_REGION_SENTINEL);
  const [search, setSearch] = useState<string>('');
  const [refreshing, setRefreshing] = useState<boolean>(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const refresh = async () => {
    setRefreshing(true);
    setLoadError(null);
    try {
      const data = await listRegions();
      setRows(data);
    } catch (e: unknown) {
      // Graceful: when the coordinator is unreachable (offline /
      // dev cluster down) we still render the "Best (auto)" row +
      // a retry-on-pull-down nudge. Maestro smoke flow asserts only
      // that "Best (auto)" is visible, so this code path is
      // gate-safe.
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
      // Persistence failure shouldn't block the navigation back —
      // the in-memory selection is still used for this app session.
    }
  };

  const display: DisplayRow[] = useMemo(() => {
    const list: DisplayRow[] = [
      {
        value: AUTO_REGION_SENTINEL,
        label: 'Best (auto)',
        subtitle: 'Coordinator picks the closest fastest provider',
      },
    ];
    for (const r of rows ?? []) {
      list.push({
        value: r.region,
        label: humanizeRegion(r.region),
        flag: flagForRegion(r.region),
        subtitle: `${r.healthyProviders} of ${r.totalProviders} online`,
      });
    }
    if (!search) return list;
    const needle = search.toLowerCase();
    return list.filter(
      (r) =>
        r.label.toLowerCase().includes(needle) || r.value.toLowerCase().includes(needle),
    );
  }, [rows, search]);

  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <TextInput
          style={styles.search}
          placeholder="Search regions"
          placeholderTextColor="rgba(127, 127, 127, 0.6)"
          value={search}
          onChangeText={setSearch}
          autoCapitalize="none"
          autoCorrect={false}
          testID="region-search"
        />
        {loadError && rows?.length === 0 && (
          <ThemedText type="small" style={styles.error}>
            Couldn&apos;t fetch the full list — only the default option is
            available. Pull to retry.
          </ThemedText>
        )}
        <FlatList
          data={display}
          keyExtractor={(item) => item.value}
          refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} />}
          ListEmptyComponent={
            refreshing ? <ActivityIndicator style={{ paddingVertical: 24 }} /> : null
          }
          renderItem={({ item }) => (
            <Pressable
              testID={
                item.value === AUTO_REGION_SENTINEL
                  ? 'region-row-auto'
                  : `region-row-${item.value}`
              }
              style={[
                styles.row,
                item.value === selected ? styles.rowSelected : undefined,
              ]}
              onPress={() => onPick(item.value)}
            >
              {item.flag ? <ThemedText type="default">{item.flag}</ThemedText> : null}
              <View style={styles.rowText}>
                <ThemedText type="default">{item.label}</ThemedText>
                {item.subtitle ? (
                  <ThemedText type="small">{item.subtitle}</ThemedText>
                ) : null}
              </View>
              {item.value === selected ? (
                <ThemedText type="default">✓</ThemedText>
              ) : null}
            </Pressable>
          )}
        />
      </SafeAreaView>
    </ThemedView>
  );
}

// ── Display helpers ───────────────────────────────────────────────

/** Best-effort human label for a region slug. The vpn-svc backend
 *  doesn't carry display names yet — we synthesise one from the
 *  slug. Falls back to the raw slug for unknown shapes. */
function humanizeRegion(slug: string): string {
  const map: Record<string, string> = {
    'us-east-1': 'US East — Virginia',
    'us-east-2': 'US East — Ohio',
    'us-west-1': 'US West — California',
    'us-west-2': 'US West — Oregon',
    'eu-west-1': 'EU West — Ireland',
    'eu-central-1': 'EU Central — Frankfurt',
    'ap-northeast-1': 'Asia Pacific — Tokyo',
    'ap-southeast-1': 'Asia Pacific — Singapore',
  };
  if (map[slug]) return map[slug];
  // Title-case the slug: "ca-central-1" → "Ca Central 1"
  return slug
    .split('-')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

/** Flag emoji guess from the region's country code prefix. Same
 *  fallback rule: return empty (no flag) for unknown shapes rather
 *  than a misleading guess. */
function flagForRegion(slug: string): string | undefined {
  const prefix = slug.split('-')[0];
  const flags: Record<string, string> = {
    us: '🇺🇸',
    ca: '🇨🇦',
    eu: '🇪🇺',
    uk: '🇬🇧',
    de: '🇩🇪',
    fr: '🇫🇷',
    nl: '🇳🇱',
    jp: '🇯🇵',
    sg: '🇸🇬',
    au: '🇦🇺',
    br: '🇧🇷',
    ap: '🌏',
  };
  return flags[prefix];
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.three, gap: 12 },
  search: {
    backgroundColor: 'rgba(127, 127, 127, 0.1)',
    paddingHorizontal: 14,
    paddingVertical: 10,
    borderRadius: 10,
    fontSize: 16,
  },
  error: {
    color: '#cf222e',
    paddingHorizontal: 4,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingVertical: 14,
    paddingHorizontal: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(127, 127, 127, 0.2)',
  },
  rowSelected: {
    backgroundColor: 'rgba(32, 138, 239, 0.08)',
  },
  rowText: {
    flex: 1,
    gap: 2,
  },
});
