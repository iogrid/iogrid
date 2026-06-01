// Region picker — stub for #567 bootstrap so the Maestro smoke flow's
// `region-picker-row` tap lands on a screen with "Best (auto)" text.
// Full implementation (search, flags, server-fetched list, persisted
// selection) lives in #571.

import { StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';

export default function RegionsScreen() {
  return (
    <ThemedView style={styles.container}>
      <SafeAreaView style={styles.safe}>
        <ThemedText type="title">Region</ThemedText>
        <View style={styles.row}>
          <ThemedText type="default">Best (auto)</ThemedText>
          <ThemedText type="default">✓</ThemedText>
        </View>
        <ThemedText type="small">
          More regions land in #571 — for now the coordinator picks the
          lowest-latency provider across all regions.
        </ThemedText>
      </SafeAreaView>
    </ThemedView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  safe: { flex: 1, paddingHorizontal: Spacing.three, gap: 16 },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(127, 127, 127, 0.2)',
  },
});
