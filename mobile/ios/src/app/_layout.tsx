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

const ONBOARDED_FLAG_KEY = 'iogrid.onboarded';

function AuthGate() {
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    AsyncStorage.getItem(ONBOARDED_FLAG_KEY)
      .then((flag) => {
        if (!flag) {
          // First launch — route into onboarding.
          router.replace('/(onboarding)/welcome' as any);
        }
        setChecked(true);
      })
      .catch(() => {
        // Storage failure: default to onboarding so the user sees the
        // welcome screen rather than a confusing main screen with stub
        // data.
        router.replace('/(onboarding)/welcome' as any);
        setChecked(true);
      });
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
        <Stack.Screen name="regions" options={{ title: 'Region' }} />
        <Stack.Screen name="settings" options={{ title: 'Settings' }} />
        <Stack.Screen
          name="(onboarding)"
          options={{ headerShown: false, gestureEnabled: false }}
        />
        <Stack.Screen name="topup" options={{ title: 'Top up', presentation: 'modal' }} />
      </Stack>
    </ThemeProvider>
  );
}
