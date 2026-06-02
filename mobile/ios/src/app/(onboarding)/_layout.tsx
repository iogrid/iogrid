/**
 * Onboarding stack — hidden nav, swipeable through the welcome → privacy
 * → sign-in sequence. Once the user signs in, Track 1's auth.ts writes
 * a JWT to Keychain and the root layout (mobile/ios/src/app/_layout.tsx)
 * skips this group entirely on subsequent launches.
 *
 * Refs #580, #590.
 */

import { Stack } from 'expo-router';

export default function OnboardingLayout() {
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        animation: 'slide_from_right',
        gestureEnabled: true,
      }}
    >
      <Stack.Screen name="welcome" />
      <Stack.Screen name="privacy" />
      <Stack.Screen name="sign-in-with-apple" />
    </Stack>
  );
}
