/**
 * Onboarding Screen 2 — "A VPN powered by people, not data centers"
 *
 * Per mobile/ios/docs/ux-wireframes-v2.md Screen 2. Sets the iogrid
 * narrative: residential exit IPs from real people who rent idle
 * bandwidth, paid in $GRID tokens.
 *
 * Refs #580, #590.
 */

import { Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export default function WelcomeScreen() {
  const theme = useTheme();

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'bottom', 'left', 'right']}>
        {/* ── Page indicator + skip ────────────────────────────── */}
        <View style={styles.topBar}>
          <View style={styles.dots}>
            <View
              testID="onboarding-dot-0"
              style={[styles.dot, styles.dotActive, { backgroundColor: theme.text }]}
            />
            <View
              style={[styles.dot, { backgroundColor: theme.border }]}
            />
          </View>
          <Pressable
            testID="onboarding-skip"
            onPress={() => router.replace('/(onboarding)/sign-in-with-apple')}
            hitSlop={12}
            accessibilityLabel="Skip onboarding"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.skip, { color: theme.textSecondary }]}>
              Skip
            </ThemedText>
          </Pressable>
        </View>

        {/* ── Illustration block (ASCII peer-mesh stylization) ── */}
        <View style={styles.illustration}>
          <View
            style={[
              styles.peerCluster,
              { backgroundColor: theme.backgroundElement, borderColor: theme.border },
            ]}
          >
            <View style={styles.peerRow}>
              <Peer label="🏠" theme={theme} />
              <Peer label="🏠" theme={theme} />
              <Peer label="🏠" theme={theme} />
            </View>
            <ThemedText style={[styles.connector, { color: theme.textTertiary }]}>
              │  │  │
            </ThemedText>
            <ThemedText style={[styles.connector, { color: theme.textTertiary }]}>
              ╲ │ ╱
            </ThemedText>
            <ThemedText style={[styles.connector, { color: theme.textTertiary }]}>
              ▼
            </ThemedText>
            <View
              style={[
                styles.youNode,
                { backgroundColor: theme.background, borderColor: theme.text },
              ]}
            >
              <ThemedText style={[styles.youLabel, { color: theme.text }]}>
                you
              </ThemedText>
            </View>
          </View>
        </View>

        {/* ── Copy ─────────────────────────────────────────────── */}
        <View style={styles.copy}>
          <ThemedText style={[styles.headline, { color: theme.text }]}>
            A VPN powered by{'\n'}people, not data centers
          </ThemedText>
          <ThemedText style={[styles.body, { color: theme.textSecondary }]}>
            iogrid routes your traffic through real homes from real people
            who rent their idle bandwidth. Pay only for what you use,
            in $GRID tokens.
          </ThemedText>
        </View>

        {/* ── Primary CTA ──────────────────────────────────────── */}
        <View style={styles.ctaWrap}>
          <Pressable
            testID="onboarding-continue"
            onPress={() => router.push('/(onboarding)/privacy')}
            style={({ pressed }) => [
              styles.cta,
              { backgroundColor: theme.text },
              pressed ? styles.ctaPressed : null,
            ]}
            accessibilityLabel="Continue to next screen"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.ctaLabel, { color: theme.textInverse }]}>
              Continue
            </ThemedText>
          </Pressable>
        </View>
      </SafeAreaView>
    </ThemedView>
  );
}

interface PeerProps {
  label: string;
  theme: ReturnType<typeof useTheme>;
}

function Peer({ label, theme }: PeerProps) {
  return (
    <View
      style={[
        styles.peerNode,
        { backgroundColor: theme.background, borderColor: theme.borderStrong },
      ]}
    >
      <ThemedText style={styles.peerLabel}>{label}</ThemedText>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  safe: { flex: 1 },
  topBar: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.md,
  },
  dots: {
    flexDirection: 'row',
    gap: Spacing.sm,
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  dotActive: {
    width: 24,
  },
  skip: {
    ...TypeScale.bodyM,
    fontWeight: '500',
  },
  illustration: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: Spacing.xl,
  },
  peerCluster: {
    alignItems: 'center',
    paddingVertical: Spacing.xl,
    paddingHorizontal: Spacing.xxl,
    borderRadius: 24,
    borderWidth: StyleSheet.hairlineWidth,
    gap: Spacing.sm,
  },
  peerRow: {
    flexDirection: 'row',
    gap: Spacing.md,
    marginBottom: Spacing.sm,
  },
  peerNode: {
    width: 56,
    height: 56,
    borderRadius: 28,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: 'center',
    justifyContent: 'center',
  },
  peerLabel: {
    fontSize: 24,
  },
  connector: {
    ...TypeScale.monoM,
    letterSpacing: 4,
  },
  youNode: {
    width: 80,
    height: 56,
    borderRadius: 28,
    borderWidth: 2,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: Spacing.sm,
  },
  youLabel: {
    ...TypeScale.bodyL,
    fontWeight: '600',
  },
  copy: {
    paddingHorizontal: Spacing.xl,
    gap: Spacing.md,
    marginBottom: Spacing.xl,
  },
  headline: {
    ...TypeScale.displayM,
  },
  body: {
    ...TypeScale.bodyM,
  },
  ctaWrap: {
    paddingHorizontal: Spacing.lg,
    paddingBottom: Spacing.lg,
  },
  cta: {
    paddingVertical: Spacing.lg,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 56,
  },
  ctaPressed: {
    opacity: 0.85,
  },
  ctaLabel: {
    ...TypeScale.button,
  },
});
