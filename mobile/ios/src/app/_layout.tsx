// Root navigation — Stack with auth gate (#582 / EPIC #581).
//
// Cold launch flow:
//   1. RootLayout mounts; AuthGate runs once.
//   2. AuthGate reads the persisted iogrid session from Keychain
//      via lib/auth.readPersistedSession().
//   3. If a non-expired session exists → stay on the existing route
//      (typically /, the VPN toggle).
//   4. If no session → router.replace into the onboarding stack
//      where Sign-in-with-Apple gates progress.
//
// We don't make AuthGate render a sign-in screen itself — we use
// Expo Router's group convention `(onboarding)` so the gate is just
// a navigation decision, and the actual screen lives in its own
// file under src/app/(onboarding)/sign-in-with-apple.tsx.
//
// The original Stack screen layout (toggle / regions / settings) is
// preserved verbatim so Maestro flows + #567/#568/#569 wiring keep
// working — the auth gate is additive, not a replacement.

import { useEffect, useState } from 'react';
import { DarkTheme, DefaultTheme, Stack, ThemeProvider, router } from 'expo-router';
import { useColorScheme } from 'react-native';

import { AnimatedSplashOverlay } from '@/components/animated-icon';
import { readPersistedSession } from '@/lib/auth';

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
          options={{ headerShown: false, presentation: 'modal' }}
        />
      </Stack>
    </ThemeProvider>
  );
}

// AuthGate: on first mount, decide whether to push the onboarding
// stack or stay on the main app. Re-runs only on cold launch — once
// a session is established, the gate leaves further navigation
// alone (sign-out routes back to /(onboarding)/sign-in-with-apple
// from the settings screen explicitly).
function AuthGate() {
  const [checked, setChecked] = useState(false);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const session = await readPersistedSession();
        if (cancelled) return;
        if (!session) {
          router.replace('/(onboarding)/sign-in-with-apple');
        }
      } finally {
        if (!cancelled) setChecked(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);
  // Renders nothing — pure side-effect component.
  void checked;
  return null;
}
