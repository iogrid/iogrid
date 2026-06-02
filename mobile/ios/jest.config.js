// Jest config for mobile/ios — minimal pure-JS / node-env setup.
//
// Goal: exercise the pure-TS modules under src/lib (wallet deeplink
// builders + grid_balance RPC code) WITHOUT pulling in the full
// react-native / expo-runtime test stack (jest-expo, metro-babel,
// react-native preset). Those presets weigh ~150 MiB combined and we
// only need to test deeplink string construction + fetch-based RPC
// edge cases. Native-shim modules (expo-linking, expo-crypto,
// expo-secure-store, expo-router) are mocked via `moduleNameMapper`
// in src/lib/mocks/.
//
// Run with `npx jest --config jest.config.js` from mobile/ios/.
// The harness uses ts-jest's default ESM-aware TS transform with
// `isolatedModules: true` so the tests run without needing the full
// @types/react RN ambient declarations to resolve.

/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'node',
  testMatch: ['<rootDir>/src/**/__tests__/**/*.test.ts'],
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx', 'json'],
  transform: {
    '^.+\\.tsx?$': [
      'ts-jest',
      {
        diagnostics: false,
        tsconfig: {
          module: 'commonjs',
          target: 'es2020',
          esModuleInterop: true,
          allowSyntheticDefaultImports: true,
          isolatedModules: true,
          strict: false,
          skipLibCheck: true,
          jsx: 'react',
          types: [],
        },
      },
    ],
  },
  // Native-shim modules don't load under node — redirect to the local
  // mocks in src/lib/mocks/. These also serve as the runtime contract
  // tests assert against (e.g. capturing the openURL() argument).
  moduleNameMapper: {
    '^expo-linking$': '<rootDir>/src/lib/mocks/expo-linking.ts',
    '^expo-crypto$': '<rootDir>/src/lib/mocks/expo-crypto.ts',
  },
  // Default 5s — bump because the retry-with-backoff test waits for
  // setTimeout(0) batches.
  testTimeout: 10000,
  clearMocks: true,
};
