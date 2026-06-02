// Sign in with Apple — the ONLY auth path on iOS v1.
//
// Closes #582 (Track 1 of EPIC #581). Layout mirrors the Screen 3
// wireframe in mobile/ios/docs/ux-wireframes-v2.md: centered logo
// over a tagline, with Apple's canonical sign-in button anchored
// near the bottom safe-area. On error we surface a banner + a
// "Try again" button without leaving the screen.
//
// Apple's Human Interface Guidelines REQUIRE us to use the system-
// rendered button (AppleAuthentication.AppleAuthenticationButton)
// rather than a custom-drawn Pressable — App Store review rejects
// custom Apple sign-in buttons. We honor that here.
//
// Track 4 (#590) owns the surrounding onboarding shell (welcome
// copy, what-you-get bullets, paywall preview); this screen is the
// embeddable auth-gate they wrap.

import { useCallback, useState } from 'react';
import { ActivityIndicator, Platform, Pressable, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router } from 'expo-router';
import * as AppleAuthentication from 'expo-apple-authentication';

import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { Spacing } from '@/constants/theme';
import { signInWithApple, AuthError } from '@/lib/auth';

type ScreenState =
  | { kind: 'idle' }
  | { kind: 'signing-in' }
  | { kind: 'error'; message: string };

export default function SignInWithAppleScreen() {
  const [state, setState] = useState<ScreenState>({ kind: 'idle' });

  const onPress = useCallback(async () => {
    setState({ kind: 'signing-in' });
    try {
      await signInWithApple();
      // Route to the home screen — main app surface.
      router.replace('/');
    } catch (err) {
      if (err instanceof AuthError && err.code === 'apple_canceled') {
        // User dismissed the sheet — no error banner, just reset.
        setState({ kind: 'idle' });
        return;
      }
      const message =
        err instanceof AuthError
          ? friendlyMessage(err)
          : 'Apple sign-in failed. Please try again.';
      setState({ kind: 'error', message });
    }
  }, []);

  return (
    <ThemedView style={styles.root}>
      <SafeAreaView style={styles.safe} edges={['top', 'bottom']}>
        {/* Top spacer + brand block */}
        <View style={styles.brandBlock}>
          <ThemedText type="title" style={styles.title} testID="signin-title">
            iogrid
          </ThemedText>
          <ThemedText type="default" themeColor="textSecondary" style={styles.tagline}>
            Fast, anonymous VPN.{'\n'}Powered by a peer-to-peer mesh.
          </ThemedText>
        </View>

        {/* CTA block */}
        <View style={styles.ctaBlock}>
          {state.kind === 'error' && (
            <View style={styles.errorBanner} testID="signin-error">
              <ThemedText type="small" style={styles.errorText}>
                {state.message}
              </ThemedText>
            </View>
          )}

          {Platform.OS === 'ios' ? (
            <AppleAuthentication.AppleAuthenticationButton
              buttonType={AppleAuthentication.AppleAuthenticationButtonType.SIGN_IN}
              buttonStyle={AppleAuthentication.AppleAuthenticationButtonStyle.BLACK}
              cornerRadius={12}
              style={styles.appleButton}
              onPress={onPress}
            />
          ) : (
            // Non-iOS platforms get a stub so the screen still renders
            // on Android / web targets. v1 ships iOS-only sign-in.
            <Pressable
              testID="signin-with-apple-stub"
              onPress={onPress}
              style={styles.fallbackButton}
            >
              <ThemedText type="default" style={styles.fallbackButtonText}>
                Sign in with Apple
              </ThemedText>
            </Pressable>
          )}

          {state.kind === 'signing-in' && (
            <View style={styles.spinnerWrap} testID="signin-spinner">
              <ActivityIndicator />
            </View>
          )}

          {state.kind === 'error' && (
            <Pressable
              testID="signin-retry"
              onPress={onPress}
              style={styles.retryButton}
            >
              <ThemedText type="default" style={styles.retryText}>
                Try again
              </ThemedText>
            </Pressable>
          )}

          <ThemedText type="small" themeColor="textSecondary" style={styles.legal}>
            By continuing, you agree to the iogrid Terms of Service and
            Privacy Policy.
          </ThemedText>
        </View>
      </SafeAreaView>
    </ThemedView>
  );
}

function friendlyMessage(err: AuthError): string {
  switch (err.code) {
    case 'apple_failed':
      return 'Apple sign-in failed. Please try again.';
    case 'server_rejected':
      return 'Sign-in was rejected. Please try again.';
    case 'server_unreachable':
      return 'Could not reach the iogrid servers. Check your connection and try again.';
    default:
      return 'Apple sign-in failed. Please try again.';
  }
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
  safe: {
    flex: 1,
    paddingHorizontal: Spacing.four,
  },
  brandBlock: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: Spacing.three,
  },
  title: {
    fontSize: 48,
    fontWeight: '700',
  },
  tagline: {
    textAlign: 'center',
    fontSize: 16,
    lineHeight: 22,
  },
  ctaBlock: {
    gap: Spacing.three,
    paddingBottom: Spacing.four,
  },
  errorBanner: {
    backgroundColor: '#FEE2E2',
    borderRadius: 8,
    padding: Spacing.three,
  },
  errorText: {
    color: '#991B1B',
    textAlign: 'center',
  },
  appleButton: {
    height: 52,
  },
  fallbackButton: {
    height: 52,
    borderRadius: 12,
    backgroundColor: '#000000',
    alignItems: 'center',
    justifyContent: 'center',
  },
  fallbackButtonText: {
    color: '#FFFFFF',
    fontWeight: '600',
  },
  spinnerWrap: {
    paddingVertical: Spacing.two,
    alignItems: 'center',
  },
  retryButton: {
    paddingVertical: Spacing.two,
    alignItems: 'center',
  },
  retryText: {
    fontWeight: '600',
  },
  legal: {
    textAlign: 'center',
  },
});
