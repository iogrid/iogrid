// Apple sign-in mock — returns a fixed identity payload so the Maestro
// onboarding flow can drive past the system sheet without invoking
// Apple's auth UI (Maestro can't fingerprint reliably).

export interface MockAppleCredential {
  user: string;
  identityToken: string;
  email: string;
  fullName: { givenName: string; familyName: string };
}

export const MOCK_APPLE_CREDENTIAL: MockAppleCredential = {
  user: 'mock.apple.user.000000.0000000000000000',
  identityToken: 'mock-identity-token',
  email: 'mock@example.com',
  fullName: { givenName: 'Maestro', familyName: 'Tester' },
};

export async function mockAppleSignIn(): Promise<MockAppleCredential> {
  await new Promise((r) => setTimeout(r, 100));
  return MOCK_APPLE_CREDENTIAL;
}
