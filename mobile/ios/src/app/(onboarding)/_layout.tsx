// Onboarding stack — minimal, headerless. Each onboarding screen owns
// its own visuals (logo, copy, CTA) so the navigation chrome doesn't
// compete for attention.
//
// Today this stack contains only the Sign-in-with-Apple screen. Track 4
// (#590) layers the rest of the onboarding (welcome, region picker
// preview, paywall preview, etc.) and wraps this screen as the auth
// gate that must complete before the home screen renders.

import { Stack } from 'expo-router';

export default function OnboardingLayout() {
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        // Modal-ish presentation looks right on iOS for "you must
        // complete this to continue" flows. Track 4 may override.
        animation: 'fade',
      }}
    />
  );
}
