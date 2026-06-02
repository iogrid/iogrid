// Onboarding stack — headerless, no swipe-back gesture so users can't
// half-quit the flow. Two screens (welcome + privacy) + two stub
// screens owned by Track 1 (sign-in-with-apple) and Track 2
// (connect-wallet).

import { Stack } from 'expo-router';

export default function OnboardingLayout() {
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        gestureEnabled: false,
        animation: 'slide_from_right',
      }}
    />
  );
}
