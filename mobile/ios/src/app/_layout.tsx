// Root navigation — Stack with an onboarding branch.
//
// First-launch flow: read AsyncStorage 'iogrid.onboarded'. If unset, the
// router lands on /(onboarding)/welcome. If set, lands on /index.
//
// Note: Tracks 1+2 also edit this file (auth gate + wallet gate). Merge
// conflicts EXPECTED; resolve in PR by combining gating logic.

import { useEffect, useState } from 'react';
import { DarkTheme, DefaultTheme, Stack, ThemeProvider, useRouter } from 'expo-router';
import { useColorScheme } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';

import { AnimatedSplashOverlay } from '@/components/animated-icon';
import { ONBOARDED_KEY } from '@/lib/auth';

export default function RootLayout() {
  const colorScheme = useColorScheme();
  const router = useRouter();
  const [, setChecked] = useState(false);

  useEffect(() => {
    let cancelled = false;
    AsyncStorage.getItem(ONBOARDED_KEY)
      .then((flag) => {
        if (cancelled) return;
        if (!flag) {
          // Defer the navigation until the Stack has mounted — calling
          // router.replace synchronously during first render is a no-op
          // in expo-router because the navigator isn't ready yet.
          requestAnimationFrame(() => {
            router.replace('/(onboarding)/welcome');
          });
        }
        setChecked(true);
      })
      .catch(() => setChecked(true));
    return () => {
      cancelled = true;
    };
  }, [router]);

  return (
    <ThemeProvider value={colorScheme === 'dark' ? DarkTheme : DefaultTheme}>
      <AnimatedSplashOverlay />
      <Stack
        screenOptions={{
          headerShown: false,
          contentStyle: { backgroundColor: colorScheme === 'dark' ? '#000' : '#fff' },
        }}
      >
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen
          name="(onboarding)"
          options={{ headerShown: false, gestureEnabled: false }}
        />
        <Stack.Screen
          name="regions"
          options={{ headerShown: true, title: 'Choose region' }}
        />
        <Stack.Screen
          name="settings"
          options={{ headerShown: true, title: 'Settings' }}
        />
        <Stack.Screen
          name="topup"
          options={{ headerShown: true, title: 'Top up', presentation: 'modal' }}
        />
      </Stack>
    </ThemeProvider>
  );
}
