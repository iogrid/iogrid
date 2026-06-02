// Authentication interface — Track 1 owns the implementation.
//
// Track 4 (this PR) declares the contract + a stub implementation that
// delegates to the Maestro mock when MAESTRO_MODE=1, so the UI screens
// can render + the smoke flows can drive through onboarding without
// Track 1 blocking the merge.
//
// When Track 1 lands, the body of `signInWithApple()` calls the native
// AuthenticationServices ASAuthorizationAppleIDProvider via
// expo-apple-authentication. Merge conflict resolution: keep Track 1's
// `signInWithApple` body; this file's `getStoredSession` / `signOut`
// shape stays the same.

import AsyncStorage from '@react-native-async-storage/async-storage';

import { isMaestroMode, mockAppleSignIn } from '@/lib/mocks';

export const ONBOARDED_KEY = 'iogrid.onboarded';
export const APPLE_USER_KEY = 'iogrid.auth.appleUser';
export const APPLE_EMAIL_KEY = 'iogrid.auth.appleEmail';

export interface AppleSession {
  appleUserId: string;
  email: string;
  identityToken: string;
}

export interface AuthApi {
  signInWithApple(): Promise<AppleSession>;
  signOut(): Promise<void>;
  getStoredSession(): Promise<AppleSession | null>;
}

async function persist(session: AppleSession): Promise<void> {
  await Promise.all([
    AsyncStorage.setItem(APPLE_USER_KEY, session.appleUserId),
    AsyncStorage.setItem(APPLE_EMAIL_KEY, session.email),
  ]);
}

export const auth: AuthApi = {
  async signInWithApple(): Promise<AppleSession> {
    if (isMaestroMode()) {
      const mock = await mockAppleSignIn();
      const session: AppleSession = {
        appleUserId: mock.user,
        email: mock.email,
        identityToken: mock.identityToken,
      };
      await persist(session);
      return session;
    }
    // Track 1 wires the real ASAuthorizationAppleIDProvider call here.
    // For now we return a placeholder so the UI surface renders without
    // throwing; the production build won't ship this code path because
    // Track 1's merge replaces it.
    const placeholder: AppleSession = {
      appleUserId: 'pending-track-1',
      email: 'pending@iogrid.org',
      identityToken: 'pending',
    };
    await persist(placeholder);
    return placeholder;
  },

  async signOut(): Promise<void> {
    await Promise.all([
      AsyncStorage.removeItem(APPLE_USER_KEY),
      AsyncStorage.removeItem(APPLE_EMAIL_KEY),
      AsyncStorage.removeItem(ONBOARDED_KEY),
    ]);
  },

  async getStoredSession(): Promise<AppleSession | null> {
    const [appleUserId, email] = await Promise.all([
      AsyncStorage.getItem(APPLE_USER_KEY),
      AsyncStorage.getItem(APPLE_EMAIL_KEY),
    ]);
    if (!appleUserId || !email) return null;
    return { appleUserId, email, identityToken: '' };
  },
};
