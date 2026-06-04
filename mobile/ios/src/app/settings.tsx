/**
 * Settings screen — v2 rewrite per mobile/ios/docs/ux-wireframes-v2.md
 * Screen 9. Drops the Mullvad-account-number model in favor of:
 *
 *   ACCOUNT      — Apple sign-in identity + bound wallet
 *   CONNECTION   — auto-connect / kill switch / DNS-leak toggles
 *   ABOUT        — privacy policy / terms / version / sign out
 *
 * Refs #580, #593.
 *
 * The Apple-email + wallet rows are wired through stub data for now;
 * Track 1 PR #601 (sign-in) and Track 2 PRs (wallets) will populate
 * the real values once they land.
 */

import { useEffect, useState } from 'react';
import { Alert, Linking, Pressable, ScrollView, StyleSheet, Switch, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Card, Radii, Spacing, TypeScale } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

const PREF_KEYS = {
  autoConnect: 'iogrid.pref.autoConnect',
  killSwitch: 'iogrid.pref.killSwitch',
  dnsLeak: 'iogrid.pref.dnsLeak',
} as const;

export default function SettingsScreen() {
  const theme = useTheme();
  const [autoConnect, setAutoConnect] = useState(false);
  const [killSwitch, setKillSwitch] = useState(true);
  const [dnsLeak, setDnsLeak] = useState(true);

  // Stub identity surface. Track 1 (#582) populates the real Apple
  // email + Track 2 (#583/#584/#585) populates wallet + balance.
  const [appleEmail, setAppleEmail] = useState<string | null>(null);
  const [walletProvider, setWalletProvider] = useState<'phantom' | 'ping' | null>(null);
  const [gridBalance, setGridBalance] = useState<number | null>(null);

  useEffect(() => {
    (async () => {
      const [auto, kill, dns] = await Promise.all([
        AsyncStorage.getItem(PREF_KEYS.autoConnect),
        AsyncStorage.getItem(PREF_KEYS.killSwitch),
        AsyncStorage.getItem(PREF_KEYS.dnsLeak),
      ]);
      if (auto != null) setAutoConnect(auto === '1');
      if (kill != null) setKillSwitch(kill === '1');
      if (dns != null) setDnsLeak(dns === '1');
    })().catch(() => undefined);
  }, []);

  const persistPref = async (key: string, value: boolean) => {
    try {
      await AsyncStorage.setItem(key, value ? '1' : '0');
    } catch {
      // Persistence failure shouldn't crash the UI; in-memory toggle
      // still takes effect for this session.
    }
  };

  const onSignOut = () => {
    Alert.alert(
      'Sign out',
      'You will need to sign in with Apple again to use iogrid. Your wallet and $GRID balance are preserved.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Sign out',
          style: 'destructive',
          onPress: async () => {
            // Track 1's auth.ts handles the Keychain clear + JWT
            // revoke. For now, navigate to the onboarding root which
            // the root layout's AuthGate will catch.
            try {
              await AsyncStorage.removeItem('iogrid.session.jwt');
            } catch {
              // ignore
            }
            router.replace('/(onboarding)/welcome' as any);
          },
        },
      ],
    );
  };

  const truncateEmail = (e: string | null) => {
    if (!e) return 'Not signed in';
    if (e.length <= 28) return e;
    return e.slice(0, 14) + '…' + e.slice(-10);
  };

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'left', 'right']}>
        {/* ── Nav header ───────────────────────────────────────── */}
        <View style={styles.navHeader}>
          <Pressable
            testID="settings-done"
            onPress={() => router.back()}
            hitSlop={12}
            accessibilityLabel="Close settings"
            accessibilityRole="button"
          >
            <ThemedText style={[styles.navLeading, { color: theme.textSecondary }]}>
              ‹ Done
            </ThemedText>
          </Pressable>
          <ThemedText style={[styles.navTitle, { color: theme.text }]}>
            Settings
          </ThemedText>
          <View style={styles.navRight} />
        </View>

        <ScrollView contentContainerStyle={styles.scroll} showsVerticalScrollIndicator={false}>
          {/* ── ACCOUNT section ──────────────────────────────────── */}
          <SectionHeader label="ACCOUNT" theme={theme} />
          <SectionGroup theme={theme}>
            <Row
              testID="settings-row-account-apple"
              theme={theme}
              label="Signed in as"
              value={truncateEmail(appleEmail)}
              chevron
              onPress={() => {
                if (!appleEmail) router.push('/(onboarding)/sign-in-with-apple' as any);
              }}
            />
            <Row
              testID="settings-row-account-wallet"
              theme={theme}
              label="Wallet"
              value={
                walletProvider
                  ? `${walletProvider === 'phantom' ? 'Phantom' : 'Ping'} · ${
                      gridBalance != null ? `${gridBalance} $GRID` : '—'
                    }`
                  : 'Not connected'
              }
              chevron
              onPress={() => router.push('/(onboarding)/connect-wallet' as any)}
            />
          </SectionGroup>

          {/* ── CONNECTION section ───────────────────────────────── */}
          <SectionHeader label="CONNECTION" theme={theme} />
          <SectionGroup theme={theme}>
            <ToggleRow
              testID="settings-row-auto-connect"
              theme={theme}
              label="Auto-connect on launch"
              value={autoConnect}
              onChange={(v) => {
                setAutoConnect(v);
                void persistPref(PREF_KEYS.autoConnect, v);
              }}
            />
            <ToggleRow
              testID="settings-row-kill-switch"
              theme={theme}
              label="Kill switch"
              hint="Block all traffic when tunnel drops"
              value={killSwitch}
              onChange={(v) => {
                setKillSwitch(v);
                void persistPref(PREF_KEYS.killSwitch, v);
              }}
            />
            <ToggleRow
              testID="settings-row-dns-leak"
              theme={theme}
              label="DNS-leak protection"
              hint="Force DNS through the tunnel"
              value={dnsLeak}
              onChange={(v) => {
                setDnsLeak(v);
                void persistPref(PREF_KEYS.dnsLeak, v);
              }}
            />
            <Row
              testID="settings-row-split-tunneling"
              theme={theme}
              label="Split tunneling"
              value="Coming soon"
              disabled
            />
          </SectionGroup>

          {/* ── ABOUT section ────────────────────────────────────── */}
          <SectionHeader label="ABOUT" theme={theme} />
          <SectionGroup theme={theme}>
            <Row
              testID="settings-row-privacy"
              theme={theme}
              label="Privacy policy"
              chevron
              onPress={() => {
                void Linking.openURL('https://iogrid.org/legal/mobile-privacy');
              }}
            />
            <Row
              testID="settings-row-terms"
              theme={theme}
              label="Terms of service"
              chevron
              onPress={() => {
                void Linking.openURL('https://iogrid.org/legal/mobile-terms');
              }}
            />
            <Row testID="settings-row-version" theme={theme} label="Version" value="1.0.0" />
          </SectionGroup>

          {/* ── Sign out ─────────────────────────────────────────── */}
          <SectionGroup theme={theme}>
            <Pressable
              testID="settings-sign-out"
              onPress={onSignOut}
              accessibilityRole="button"
              accessibilityLabel="Sign out"
              style={({ pressed }) => [
                styles.row,
                { borderBottomColor: theme.border },
                pressed ? { opacity: 0.7 } : null,
                styles.rowLast,
              ]}
            >
              <ThemedText style={[styles.rowLabel, { color: theme.error }]}>
                Sign out
              </ThemedText>
            </Pressable>
          </SectionGroup>
        </ScrollView>
      </SafeAreaView>
    </ThemedView>
  );
}

// ── Section primitives ────────────────────────────────────────────

function SectionHeader({ label, theme }: { label: string; theme: ReturnType<typeof useTheme> }) {
  return (
    <ThemedText style={[styles.sectionHeader, { color: theme.textTertiary }]}>
      {label}
    </ThemedText>
  );
}

function SectionGroup({
  theme,
  children,
}: {
  theme: ReturnType<typeof useTheme>;
  children: React.ReactNode;
}) {
  return (
    <View
      style={[
        styles.sectionGroup,
        { backgroundColor: theme.backgroundCard, borderColor: theme.border },
      ]}
    >
      {children}
    </View>
  );
}

interface RowProps {
  testID?: string;
  theme: ReturnType<typeof useTheme>;
  label: string;
  hint?: string;
  value?: string;
  chevron?: boolean;
  disabled?: boolean;
  onPress?: () => void;
}

function Row({ testID, theme, label, hint, value, chevron, disabled, onPress }: RowProps) {
  const isPressable = !!onPress && !disabled;
  const Container: typeof View | typeof Pressable = isPressable ? Pressable : View;
  return (
    <Container
      testID={testID}
      onPress={onPress}
      accessibilityLabel={label}
      accessibilityRole={isPressable ? 'button' : undefined}
      // A plain View ignores function styles — passing the Pressable-style
      // callback unconditionally left every NON-pressable row (Split
      // tunneling, Version) completely unstyled: flush-left, no inset,
      // visually broken out of its card (pass-4 capture review, #684).
      style={
        isPressable
          ? ({ pressed }: { pressed?: boolean }) => [
              styles.row,
              { borderBottomColor: theme.border },
              pressed ? { opacity: 0.7 } : null,
            ]
          : [styles.row, { borderBottomColor: theme.border }]
      }
    >
      <View style={styles.rowText}>
        <ThemedText
          style={[styles.rowLabel, { color: disabled ? theme.textTertiary : theme.text }]}
        >
          {label}
        </ThemedText>
        {hint ? (
          <ThemedText style={[styles.rowHint, { color: theme.textSecondary }]}>
            {hint}
          </ThemedText>
        ) : null}
      </View>
      <View style={styles.rowTrailing}>
        {value ? (
          <ThemedText
            style={[
              styles.rowValue,
              { color: disabled ? theme.textTertiary : theme.textSecondary },
            ]}
          >
            {value}
          </ThemedText>
        ) : null}
        {chevron ? (
          <ThemedText style={[styles.chevron, { color: theme.textTertiary }]}>›</ThemedText>
        ) : null}
      </View>
    </Container>
  );
}

interface ToggleRowProps {
  testID?: string;
  theme: ReturnType<typeof useTheme>;
  label: string;
  hint?: string;
  value: boolean;
  onChange: (v: boolean) => void;
}

function ToggleRow({ testID, theme, label, hint, value, onChange }: ToggleRowProps) {
  return (
    <View
      testID={testID}
      accessibilityLabel={`${label} ${value ? 'enabled' : 'disabled'}`}
      accessibilityRole="switch"
      style={[styles.row, { borderBottomColor: theme.border }]}
    >
      <View style={styles.rowText}>
        <ThemedText style={[styles.rowLabel, { color: theme.text }]}>{label}</ThemedText>
        {hint ? (
          <ThemedText style={[styles.rowHint, { color: theme.textSecondary }]}>
            {hint}
          </ThemedText>
        ) : null}
      </View>
      <Switch
        value={value}
        onValueChange={onChange}
        trackColor={{ false: theme.border, true: theme.accent }}
        thumbColor={theme.background}
      />
    </View>
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
  navRight: {
    minWidth: 80,
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
  row: {
    minHeight: 56,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: Card.padding,
    paddingVertical: Spacing.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  rowLast: {
    borderBottomWidth: 0,
  },
  rowText: {
    flex: 1,
    paddingRight: Spacing.md,
  },
  rowLabel: {
    ...TypeScale.bodyL,
    fontWeight: '500',
  },
  rowHint: {
    ...TypeScale.bodyS,
    marginTop: 2,
  },
  rowTrailing: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.sm,
  },
  rowValue: {
    ...TypeScale.bodyM,
  },
  chevron: {
    fontSize: 22,
    fontWeight: '300',
  },
});
