// Root navigation — Stack instead of Tabs.
//
// VPN apps live and die at the toggle screen; tabs would dilute the
// primary affordance. Settings + Regions are pushed on top of the
// toggle, dismissed back to it. Matches Mullvad / Tailscale / iCloud
// Private Relay UX patterns.
//
// First-launch routing: AuthGate checks AsyncStorage for the
// `iogrid.onboarded` flag. Missing flag → push to
// `/(onboarding)/welcome`. Once the user completes onboarding,
// welcome.tsx (or the final onboarding step) sets the flag so
// subsequent launches go directly to the main screen.

import { useEffect, useState } from 'react';
import { DarkTheme, DefaultTheme, Stack, ThemeProvider, router } from 'expo-router';
import { useColorScheme } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { AnimatedSplashOverlay } from '@/components/animated-icon';

export const ONBOARDED_FLAG_KEY = 'iogrid.onboarded';

/**
 * checkOnboardedAndRoute — the core first-launch routing decision.
 *
 * Reads the `iogrid.onboarded` flag from AsyncStorage. If the flag is
 * missing OR the read throws (storage corruption, native-module
 * unavailable), routes to /(onboarding)/welcome. If the flag is
 * present, returns without navigating — the caller stays on whatever
 * route Expo Router resolved for the current URL.
 *
 * Exported as a standalone helper so the test suite can drive it
 * without spinning up a React renderer. AuthGate below is the
 * production wrapper that runs this inside a useEffect at mount.
 */
export async function checkOnboardedAndRoute(): Promise<void> {
  try {
    const flag = await AsyncStorage.getItem(ONBOARDED_FLAG_KEY);
    if (!flag) {
      // First launch — route into onboarding.
      router.replace('/(onboarding)/welcome' as any);
    }
  } catch {
    // Storage failure: default to onboarding so the user sees the
    // welcome screen rather than a confusing main screen with stub
    // data.
    router.replace('/(onboarding)/welcome' as any);
  }
}

export function AuthGate() {
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    checkOnboardedAndRoute().finally(() => setChecked(true));
  }, []);

  // Render nothing while checking — the splash overlay covers it.
  if (!checked) return null;
  return null;
}

export default function RootLayout() {
  const colorScheme = useColorScheme();
  return (
    <ThemeProvider value={colorScheme === 'dark' ? DarkTheme : DefaultTheme}>
      <AnimatedSplashOverlay />
      <AuthGate />
      <Stack>
        <Stack.Screen name="index" options={{ headerShown: false }} />
        {/* regions/settings/topup render their OWN in-screen headers
            (back/Done + title). The native Stack header on top of those
            produced a DOUBLE header — and leaked the previous route's
            name as the back label ("< index") because index hides its
            header but keeps its route title. headerShown:false kills
            both defects (#684; visible in the run-2 Maestro captures). */}
        <Stack.Screen name="regions" options={{ headerShown: false }} />
        <Stack.Screen name="settings" options={{ headerShown: false }} />
        <Stack.Screen
          name="(onboarding)"
          options={{ headerShown: false, gestureEnabled: false }}
        />
        <Stack.Screen
          name="topup"
          options={{ headerShown: false, presentation: 'modal' }}
        />
      </Stack>
    </ThemeProvider>
  );
}
