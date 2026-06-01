// Root navigation — Stack instead of Tabs.
//
// VPN apps live and die at the toggle screen; tabs would dilute the
// primary affordance. Settings + Regions are pushed on top of the
// toggle, dismissed back to it. Matches Mullvad / Tailscale / iCloud
// Private Relay UX patterns.

import { DarkTheme, DefaultTheme, Stack, ThemeProvider } from 'expo-router';
import { useColorScheme } from 'react-native';

import { AnimatedSplashOverlay } from '@/components/animated-icon';

export default function RootLayout() {
  const colorScheme = useColorScheme();
  return (
    <ThemeProvider value={colorScheme === 'dark' ? DarkTheme : DefaultTheme}>
      <AnimatedSplashOverlay />
      <Stack>
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="regions" options={{ title: 'Region' }} />
        <Stack.Screen name="settings" options={{ title: 'Settings' }} />
      </Stack>
    </ThemeProvider>
  );
}
